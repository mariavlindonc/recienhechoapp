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

// pedirTiempoReal manda la ruta dada al gateway y devuelve la respuesta y lo que le
// llegó a pedidos (nil si no le llegó nada).
func pedirTiempoReal(prueba *testing.T, ruta string) (*httptest.ResponseRecorder, *http.Request) {
	prueba.Helper()
	pedidos, recibidas := instanciaFalsa(prueba, "pedidos", 0)
	gin.SetMode(gin.TestMode)
	router := nuevoRouter([]byte(secretoDePrueba), http.NotFoundHandler(), nuevoProxyBalanceado("pedidos", []*url.URL{pedidos}, time.Second, nil), http.NotFoundHandler(), http.NotFoundHandler())
	respuesta := httptest.NewRecorder()
	router.ServeHTTP(respuesta, httptest.NewRequest(http.MethodGet, ruta, nil))
	if len(*recibidas) == 0 {
		return respuesta, nil
	}
	return respuesta, (*recibidas)[0]
}

// El navegador no puede mandar Authorization en un WebSocket: el token va en la URL,
// y no sigue viaje, para que no quede en ningún log (ADR-018).
func TestElWebSocketSeAutenticaConElTokenDeLaURL(prueba *testing.T) {
	token := firmar(prueba, secretoDePrueba, jwt.SigningMethodHS256, time.Now().Add(time.Hour))

	_, recibida := pedirTiempoReal(prueba, "/pedidos/tiempo-real?token="+token)

	if recibida == nil {
		prueba.Fatal("la solicitud no llegó a pedidos")
	}
	if recibida.Header.Get("X-Id-Usuario") != "id-ana" || recibida.Header.Get("X-Rol-Usuario") != "comprador" {
		prueba.Errorf("llegó la identidad %v, se esperaba la del token", recibida.Header)
	}
	if recibida.URL.RawQuery != "" {
		prueba.Errorf("el token no puede llegar a pedidos: query %q", recibida.URL.RawQuery)
	}
}

func TestElTokenDeLaURLSeVerificaComoLosDemas(prueba *testing.T) {
	casos := map[string]struct{ token, codigo string }{
		"vencido":      {firmar(prueba, secretoDePrueba, jwt.SigningMethodHS256, time.Now().Add(-time.Minute)), "sesion_vencida"},
		"otro secreto": {firmar(prueba, "otro-secreto-de-al-menos-32-bytes!!", jwt.SigningMethodHS256, time.Now().Add(time.Hour)), "sin_sesion"},
		"basura":       {"abc.def.ghi", "sin_sesion"},
	}
	for nombre, caso := range casos {
		prueba.Run(nombre, func(prueba *testing.T) {
			respuesta, recibida := pedirTiempoReal(prueba, "/pedidos/tiempo-real?token="+caso.token)
			if recibida != nil || respuesta.Code != http.StatusUnauthorized || !strings.Contains(respuesta.Body.String(), caso.codigo) {
				prueba.Errorf("estado %d, cuerpo %s, llegó a pedidos: %v", respuesta.Code, respuesta.Body, recibida != nil)
			}
		})
	}
}

// El token en la URL vale solo en esta ruta: en las demás va en Authorization.
func TestElTokenEnLaURLNoValeEnOtraRuta(prueba *testing.T) {
	token := firmar(prueba, secretoDePrueba, jwt.SigningMethodHS256, time.Now().Add(time.Hour))

	_, recibida := pedirTiempoReal(prueba, "/pedidos?token="+token)

	if recibida == nil {
		prueba.Fatal("la solicitud no llegó a pedidos")
	}
	if id := recibida.Header.Get("X-Id-Usuario"); id != "" {
		prueba.Errorf("X-Id-Usuario = %q: en otra ruta el token de la URL no identifica a nadie", id)
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
	fmt.Fprintf(conexion, "GET /pedidos/tiempo-real?token=%s HTTP/1.1\r\nHost: gateway\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", token)

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
		prueba.Error("/pedidos/tiempo-real no se traza: la URL lleva el token")
	}
	if !sinChequeosDeSalud(httptest.NewRequest(http.MethodGet, "/pedidos", nil)) {
		prueba.Error("las demás rutas se trazan")
	}
}
