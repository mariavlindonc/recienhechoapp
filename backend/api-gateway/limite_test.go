package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestLimitePorGrupo(prueba *testing.T) {
	reloj := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	limite := nuevoLimitador(2, time.Minute, func() time.Time { return reloj })
	claves := map[string]string{"clave-a1": "grupo-a", "clave-a2": "grupo-a", "clave-b": "grupo-b"}
	reenviadas := 0
	manejador := limitarPorGrupo(limite, claves, http.HandlerFunc(func(escritor http.ResponseWriter, _ *http.Request) {
		reenviadas++
	}))
	pedir := func(clave string) *httptest.ResponseRecorder {
		pedido := httptest.NewRequest(http.MethodGet, "/notificaciones/id-1", nil)
		pedido.Header.Set("X-API-Key", clave)
		respuesta := httptest.NewRecorder()
		manejador.ServeHTTP(respuesta, pedido)
		return respuesta
	}

	// Las dos claves del grupo a suman.
	pedir("clave-a1")
	pedir("clave-a2")
	reloj = reloj.Add(20 * time.Second)
	respuesta := pedir("clave-a1")

	var cuerpo map[string]string
	if respuesta.Code != http.StatusTooManyRequests || json.Unmarshal(respuesta.Body.Bytes(), &cuerpo) != nil || cuerpo["codigo"] != "limite_superado" {
		prueba.Fatalf("se esperaba 429 limite_superado, llegó %d %s", respuesta.Code, respuesta.Body.String())
	}
	if espera := respuesta.Header().Get("Retry-After"); espera != "40" {
		prueba.Errorf("Retry-After = %q, se esperaba 40", espera)
	}
	if otroGrupo := pedir("clave-b"); otroGrupo.Code != http.StatusOK {
		prueba.Errorf("el límite es por grupo: el grupo b recibió %d", otroGrupo.Code)
	}
	// Una clave desconocida no cuenta: la rechaza notificaciones.
	pedir("desconocida")
	if reenviadas != 4 {
		prueba.Errorf("se reenviaron %d, se esperaban 4 (a1, a2, b y la desconocida)", reenviadas)
	}

	reloj = reloj.Add(40 * time.Second)
	if nuevaVentana := pedir("clave-a1"); nuevaVentana.Code != http.StatusOK {
		prueba.Errorf("en la ventana siguiente el grupo a recibió %d", nuevaVentana.Code)
	}
}

// El inicio de sesión y el registro tienen un límite por IP: frena a quien prueba
// contraseñas y protege la CPU, que bcrypt usa en cada intento (ADR-019).
func TestLimiteDeIntentosPorIP(prueba *testing.T) {
	reloj := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	limite := nuevoLimitador(2, time.Minute, func() time.Time { return reloj })
	reenviadas := 0
	manejador := limitarIntentos(limite, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reenviadas++
	}))
	pedir := func(metodo, ruta, direccion string, encabezados map[string]string) *httptest.ResponseRecorder {
		pedido := httptest.NewRequest(metodo, ruta, nil)
		pedido.RemoteAddr = direccion
		for nombre, valor := range encabezados {
			pedido.Header.Set(nombre, valor)
		}
		respuesta := httptest.NewRecorder()
		manejador.ServeHTTP(respuesta, pedido)
		return respuesta
	}

	// El registro y el inicio de sesión suman para la misma IP.
	pedir(http.MethodPost, "/usuarios", "203.0.113.7:51000", nil)
	pedir(http.MethodPost, "/usuarios/sesion", "203.0.113.7:51001", nil)
	// Inventar X-Forwarded-For no cambia la IP que cuenta: es la de la conexión.
	respuesta := pedir(http.MethodPost, "/usuarios/sesion", "203.0.113.7:51002", map[string]string{"X-Forwarded-For": "198.51.100.99"})

	var cuerpo map[string]string
	if respuesta.Code != http.StatusTooManyRequests || json.Unmarshal(respuesta.Body.Bytes(), &cuerpo) != nil || cuerpo["codigo"] != "demasiados_intentos" {
		prueba.Fatalf("se esperaba 429 demasiados_intentos, llegó %d %s", respuesta.Code, respuesta.Body.String())
	}
	if respuesta.Header().Get("Retry-After") != "60" {
		prueba.Errorf("Retry-After = %q, se esperaba 60", respuesta.Header().Get("Retry-After"))
	}
	if otraIP := pedir(http.MethodPost, "/usuarios/sesion", "198.51.100.1:40000", nil); otraIP.Code != http.StatusOK {
		prueba.Errorf("otra IP tiene su propio límite: %d", otraIP.Code)
	}
	// El perfil no es un intento: no cuenta ni se frena.
	if perfil := pedir(http.MethodGet, "/usuarios/yo", "203.0.113.7:51003", nil); perfil.Code != http.StatusOK {
		prueba.Errorf("el perfil no tiene límite: %d", perfil.Code)
	}
	reloj = reloj.Add(time.Minute)
	if despues := pedir(http.MethodPost, "/usuarios/sesion", "203.0.113.7:51004", nil); despues.Code != http.StatusOK {
		prueba.Errorf("pasada la ventana se puede volver a intentar: %d", despues.Code)
	}
	if reenviadas != 5 {
		prueba.Errorf("reenviadas = %d, se esperaban 5", reenviadas)
	}
}

// Cada IP nueva ocupa lugar: las cuentas vencidas se borran, para que el mapa no
// crezca sin fin.
func TestElLimitadorBorraLasCuentasVencidas(prueba *testing.T) {
	reloj := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	limite := nuevoLimitador(5, time.Minute, func() time.Time { return reloj })
	for numero := range 100 {
		limite.permitir(fmt.Sprintf("198.51.100.%d", numero))
	}

	reloj = reloj.Add(2 * time.Minute)
	limite.permitir("203.0.113.7")

	if cantidad := len(limite.cuentas); cantidad != 1 {
		prueba.Errorf("quedaron %d cuentas, se esperaba solo la nueva", cantidad)
	}
}

// Una barra de más al final no saltea el límite.
func TestLaBarraFinalNoSalteaElLimite(prueba *testing.T) {
	limite := nuevoLimitador(1, time.Minute, time.Now)
	manejador := limitarIntentos(limite, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for _, ruta := range []string{"/usuarios/sesion", "/usuarios/sesion/", "/usuarios/"} {
		pedido := httptest.NewRequest(http.MethodPost, ruta, nil)
		pedido.RemoteAddr = "203.0.113.7:51000"
		respuesta := httptest.NewRecorder()
		manejador.ServeHTTP(respuesta, pedido)
		if ruta != "/usuarios/sesion" && respuesta.Code != http.StatusTooManyRequests {
			prueba.Errorf("%s: estado %d, se esperaba 429", ruta, respuesta.Code)
		}
	}
}

const claveFrontendDePrueba = "clave-del-frontend-de-prueba"

// routerDeIntentos arma el gateway mínimo: lee la IP del navegador y limita a 1
// intento por minuto. Devuelve también lo que le llegó al servicio.
func routerDeIntentos() (http.Handler, *[]*http.Request) {
	gin.SetMode(gin.TestMode)
	recibidas := &[]*http.Request{}
	limite := nuevoLimitador(1, time.Minute, time.Now)
	servicio := limitarIntentos(limite, http.HandlerFunc(func(_ http.ResponseWriter, pedido *http.Request) {
		*recibidas = append(*recibidas, pedido)
	}))
	router := gin.New()
	router.Use(leerIPDelNavegador(claveFrontendDePrueba))
	router.Any("/usuarios/*resto", gin.WrapH(servicio))
	return router, recibidas
}

func intentar(router http.Handler, headers map[string]string) int {
	pedido := httptest.NewRequest(http.MethodPost, "/usuarios/sesion", nil)
	pedido.RemoteAddr = "172.18.0.5:40000" // la IP del servidor del frontend
	for nombre, valor := range headers {
		pedido.Header.Set(nombre, valor)
	}
	respuesta := httptest.NewRecorder()
	router.ServeHTTP(respuesta, pedido)
	return respuesta.Code
}

// Todos los navegadores llegan desde la IP del servidor del frontend: cada uno tiene
// que contar por su propia IP, o un intento de uno frenaría a los demás (#95).
func TestCadaNavegadorTieneSuPropioLimite(prueba *testing.T) {
	router, _ := routerDeIntentos()
	navegador := func(ip string) map[string]string {
		return map[string]string{"X-IP-Navegador": ip, "X-Clave-Frontend": claveFrontendDePrueba}
	}

	if estado := intentar(router, navegador("203.0.113.1")); estado != http.StatusOK {
		prueba.Fatalf("primer navegador: %d", estado)
	}
	if estado := intentar(router, navegador("203.0.113.2")); estado != http.StatusOK {
		prueba.Errorf("otro navegador no tiene que quedar frenado: %d", estado)
	}
	if estado := intentar(router, navegador("203.0.113.1")); estado != http.StatusTooManyRequests {
		prueba.Errorf("el primero ya usó su intento: %d", estado)
	}
}

// El gateway publica su puerto: sin la clave, una IP inventada no cambia nada.
func TestLaIPDelNavegadorNoSePuedeInventar(prueba *testing.T) {
	casos := map[string]func(ip string) map[string]string{
		"sin clave": func(ip string) map[string]string { return map[string]string{"X-IP-Navegador": ip} },
		"clave errónea": func(ip string) map[string]string {
			return map[string]string{"X-IP-Navegador": ip, "X-Clave-Frontend": "otra"}
		},
	}
	for nombre, armar := range casos {
		prueba.Run(nombre, func(prueba *testing.T) {
			router, _ := routerDeIntentos()
			intentar(router, armar("198.51.100.1"))
			if estado := intentar(router, armar("198.51.100.2")); estado != http.StatusTooManyRequests {
				prueba.Errorf("estado %d: cuenta la IP de la conexión, no la inventada", estado)
			}
		})
	}
}

// Los servicios no necesitan esos headers, y la clave no tiene que llegarles.
func TestLosHeadersDelFrontendNoSiguenViaje(prueba *testing.T) {
	router, recibidas := routerDeIntentos()

	intentar(router, map[string]string{"X-IP-Navegador": "203.0.113.1", "X-Clave-Frontend": claveFrontendDePrueba})

	if len(*recibidas) != 1 {
		prueba.Fatalf("llegaron %d solicitudes", len(*recibidas))
	}
	cabeceras := (*recibidas)[0].Header
	if cabeceras.Get("X-IP-Navegador") != "" || cabeceras.Get("X-Clave-Frontend") != "" {
		prueba.Errorf("headers = %v", cabeceras)
	}
}
