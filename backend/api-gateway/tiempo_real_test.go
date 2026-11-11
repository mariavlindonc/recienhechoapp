package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// pedirConHeaders manda la ruta dada al gateway y devuelve la respuesta y lo que le
// llegó a pedidos (nil si no le llegó nada).
func pedirConHeaders(prueba *testing.T, ruta string, headers map[string]string) (*httptest.ResponseRecorder, *http.Request) {
	prueba.Helper()
	pedidos, recibidas := instanciaFalsa(prueba, "pedidos", 0)
	gin.SetMode(gin.TestMode)
	router := nuevoRouter([]byte(secretoDePrueba), http.NotFoundHandler(), nuevoProxyBalanceado("pedidos", []*url.URL{pedidos}, time.Second, nil), http.NotFoundHandler(), http.NotFoundHandler())
	pedido := httptest.NewRequest(http.MethodGet, ruta, nil)
	for nombre, valor := range headers {
		pedido.Header.Set(nombre, valor)
	}
	respuesta := httptest.NewRecorder()
	router.ServeHTTP(respuesta, pedido)
	if len(*recibidas) == 0 {
		return respuesta, nil
	}
	return respuesta, (*recibidas)[0]
}

// El navegador manda la cookie solo, también al abrir el WebSocket (ADR-019). El
// token no sigue viaje: los servicios reciben la identidad, no la sesión.
func TestLaCookieIdentificaAlUsuario(prueba *testing.T) {
	token := firmar(prueba, secretoDePrueba, jwt.SigningMethodHS256, time.Now().Add(time.Hour))

	for _, ruta := range []string{"/pedidos", "/pedidos/tiempo-real"} {
		_, recibida := pedirConHeaders(prueba, ruta, map[string]string{"Cookie": "sesion=" + token})

		if recibida == nil {
			prueba.Fatalf("%s: la solicitud no llegó a pedidos", ruta)
		}
		if recibida.Header.Get("X-Id-Usuario") != "id-ana" || recibida.Header.Get("X-Rol-Usuario") != "comprador" {
			prueba.Errorf("%s: llegó la identidad %v, se esperaba la del token", ruta, recibida.Header)
		}
		if cookie := recibida.Header.Get("Cookie"); cookie != "" {
			prueba.Errorf("%s: el token no puede llegar a los servicios: Cookie %q", ruta, cookie)
		}
	}
}

// Una cookie vieja no puede trabar a nadie: el POST de inicio de sesión también la
// lleva, así que rechazarla impediría volver a entrar. Sigue sin identidad y decide
// el servicio.
func TestUnaCookieInvalidaSigueSinIdentidad(prueba *testing.T) {
	casos := map[string]string{
		"vencida":      firmar(prueba, secretoDePrueba, jwt.SigningMethodHS256, time.Now().Add(-time.Minute)),
		"otro secreto": firmar(prueba, "otro-secreto-de-al-menos-32-bytes!!", jwt.SigningMethodHS256, time.Now().Add(time.Hour)),
		"basura":       "abc.def.ghi",
	}
	for nombre, token := range casos {
		prueba.Run(nombre, func(prueba *testing.T) {
			respuesta, recibida := pedirConHeaders(prueba, "/pedidos", map[string]string{"Cookie": "sesion=" + token})
			if recibida == nil {
				prueba.Fatalf("estado %d: la solicitud tiene que seguir", respuesta.Code)
			}
			if id := recibida.Header.Get("X-Id-Usuario"); id != "" || recibida.Header.Get("Cookie") != "" {
				prueba.Errorf("X-Id-Usuario %q, Cookie %q", id, recibida.Header.Get("Cookie"))
			}
		})
	}
}

// Quien manda Authorization (k6, app.sh) sigue igual: un token malo se rechaza.
func TestAuthorizationTienePrioridadSobreLaCookie(prueba *testing.T) {
	buena := firmar(prueba, secretoDePrueba, jwt.SigningMethodHS256, time.Now().Add(time.Hour))

	respuesta, recibida := pedirConHeaders(prueba, "/pedidos", map[string]string{"Authorization": "Bearer abc.def.ghi", "Cookie": "sesion=" + buena})

	if recibida != nil || respuesta.Code != http.StatusUnauthorized {
		prueba.Errorf("estado %d, llegó a pedidos: %v", respuesta.Code, recibida != nil)
	}
}

// El token nunca va en la URL: ahí ya no identifica a nadie (ADR-019).
func TestUnTokenEnLaURLNoIdentifica(prueba *testing.T) {
	token := firmar(prueba, secretoDePrueba, jwt.SigningMethodHS256, time.Now().Add(time.Hour))

	_, recibida := pedirConHeaders(prueba, "/pedidos/tiempo-real?token="+token, nil)

	if recibida == nil {
		prueba.Fatal("la solicitud no llegó a pedidos")
	}
	if id := recibida.Header.Get("X-Id-Usuario"); id != "" {
		prueba.Errorf("X-Id-Usuario = %q", id)
	}
}

// Un WebSocket dura minutos: no se corta a los 3 s como el resto de las solicitudes.
// El servicio falso acepta la conexión y contesta recién después del tiempo máximo.
func TestElWebSocketNoSeCortaPorElTiempoMaximo(prueba *testing.T) {
	const tiempoMaximo = 50 * time.Millisecond
	servicio := httptest.NewServer(http.HandlerFunc(func(escritor http.ResponseWriter, _ *http.Request) {
		conexion, _, err := http.NewResponseController(escritor).Hijack()
		if err != nil {
			prueba.Errorf("hijack: %v", err)
			return
		}
		defer conexion.Close()
		fmt.Fprint(conexion, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		time.Sleep(4 * tiempoMaximo)
		fmt.Fprint(conexion, "sigo acá\n")
	}))
	prueba.Cleanup(servicio.Close)
	direccion, _ := url.Parse(servicio.URL)
	gin.SetMode(gin.TestMode)
	gateway := httptest.NewServer(nuevoRouter([]byte(secretoDePrueba), http.NotFoundHandler(), nuevoProxyBalanceado("pedidos", []*url.URL{direccion}, tiempoMaximo, nil), http.NotFoundHandler(), http.NotFoundHandler()))
	prueba.Cleanup(gateway.Close)

	conexion, err := net.Dial("tcp", strings.TrimPrefix(gateway.URL, "http://"))
	if err != nil {
		prueba.Fatalf("conectar al gateway: %v", err)
	}
	defer conexion.Close()
	conexion.SetDeadline(time.Now().Add(2 * time.Second))
	token := firmar(prueba, secretoDePrueba, jwt.SigningMethodHS256, time.Now().Add(time.Hour))
	fmt.Fprintf(conexion, "GET /pedidos/tiempo-real HTTP/1.1\r\nHost: gateway\r\nCookie: sesion=%s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", token)

	lector := bufio.NewReader(conexion)
	respuesta, err := http.ReadResponse(lector, nil)
	if err != nil || respuesta.StatusCode != http.StatusSwitchingProtocols {
		prueba.Fatalf("respuesta %+v, err %v", respuesta, err)
	}
	if linea, err := lector.ReadString('\n'); err != nil && err != io.EOF || linea != "sigo acá\n" {
		prueba.Errorf("después del tiempo máximo llegó %q (%v): el gateway cortó la conexión", linea, err)
	}
}

func TestElWebSocketNoSeTraza(prueba *testing.T) {
	if sinChequeosDeSalud(httptest.NewRequest(http.MethodGet, "/pedidos/tiempo-real", nil)) {
		prueba.Error("/pedidos/tiempo-real no se traza: dura minutos")
	}
	if !sinChequeosDeSalud(httptest.NewRequest(http.MethodGet, "/pedidos", nil)) {
		prueba.Error("las demás rutas se trazan")
	}
}
