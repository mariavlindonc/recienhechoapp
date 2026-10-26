package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// instanciaFalsa simula una instancia de notificaciones: anota lo que recibe y
// responde con su nombre para saber a cuál fue cada solicitud.
func instanciaFalsa(prueba *testing.T, nombre string, demora time.Duration) (*url.URL, *[]*http.Request) {
	prueba.Helper()
	var recibidas []*http.Request
	servidor := httptest.NewServer(http.HandlerFunc(func(escritor http.ResponseWriter, pedido *http.Request) {
		cuerpo, _ := io.ReadAll(pedido.Body)
		pedido.Body = io.NopCloser(strings.NewReader(string(cuerpo)))
		recibidas = append(recibidas, pedido)
		select {
		case <-time.After(demora):
		case <-pedido.Context().Done():
			return
		}
		escritor.Header().Set("Location", "/notificaciones/id-1")
		escritor.Header().Set("X-Instancia", nombre)
		escritor.WriteHeader(http.StatusAccepted)
		escritor.Write(cuerpo)
	}))
	prueba.Cleanup(servidor.Close)
	direccion, _ := url.Parse(servidor.URL)
	return direccion, &recibidas
}

func routerDePrueba(instancias []*url.URL, tiempoMaximo time.Duration) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return nuevoRouter(nuevoProxyBalanceado("notificaciones", instancias, tiempoMaximo, nil), http.NotFoundHandler())
}

func pedir(router http.Handler, metodo, ruta, cuerpo string) *httptest.ResponseRecorder {
	pedido := httptest.NewRequest(metodo, ruta, strings.NewReader(cuerpo))
	pedido.Header.Set("X-API-Key", "clave-a")
	pedido.Header.Set("Idempotency-Key", "clave-idem-1")
	pedido.Header.Set("Content-Type", "application/json")
	respuesta := httptest.NewRecorder()
	router.ServeHTTP(respuesta, pedido)
	return respuesta
}

func TestReenviaElPedidoYLaRespuestaSinCambios(prueba *testing.T) {
	instancia, recibidas := instanciaFalsa(prueba, "uno", 0)
	router := routerDePrueba([]*url.URL{instancia}, time.Second)

	respuesta := pedir(router, http.MethodPost, "/notificaciones", `{"canal":"email"}`)

	if respuesta.Code != http.StatusAccepted || respuesta.Body.String() != `{"canal":"email"}` {
		prueba.Fatalf("respuesta = %d %q", respuesta.Code, respuesta.Body.String())
	}
	if ubicacion := respuesta.Header().Get("Location"); ubicacion != "/notificaciones/id-1" {
		prueba.Errorf("Location = %q, tenía que pasar sin cambios", ubicacion)
	}
	recibida := (*recibidas)[0]
	if recibida.URL.Path != "/notificaciones" || recibida.Method != http.MethodPost {
		prueba.Errorf("llegó %s %s", recibida.Method, recibida.URL.Path)
	}
	for _, encabezado := range []string{"X-API-Key", "Idempotency-Key", "Content-Type"} {
		if recibida.Header.Get(encabezado) == "" {
			prueba.Errorf("no llegó el header %s", encabezado)
		}
	}
	if recibida.Header.Get("X-Forwarded-For") == "" {
		prueba.Error("falta X-Forwarded-For con la IP del consumidor")
	}
}

func TestReenviaLasSubrutas(prueba *testing.T) {
	instancia, recibidas := instanciaFalsa(prueba, "uno", 0)
	router := routerDePrueba([]*url.URL{instancia}, time.Second)

	pedir(router, http.MethodGet, "/notificaciones/9f1c2a7e?x=1", "")

	if recibida := (*recibidas)[0]; recibida.URL.Path != "/notificaciones/9f1c2a7e" || recibida.URL.RawQuery != "x=1" {
		prueba.Errorf("llegó %s?%s", recibida.URL.Path, recibida.URL.RawQuery)
	}
}

func TestRoundRobinTurnaLasInstancias(prueba *testing.T) {
	uno, _ := instanciaFalsa(prueba, "uno", 0)
	dos, _ := instanciaFalsa(prueba, "dos", 0)
	router := routerDePrueba([]*url.URL{uno, dos}, time.Second)

	var turnos []string
	for range 4 {
		turnos = append(turnos, pedir(router, http.MethodGet, "/notificaciones/x", "").Header().Get("X-Instancia"))
	}

	if strings.Join(turnos, ",") != "uno,dos,uno,dos" {
		prueba.Errorf("turnos = %v, se esperaba uno,dos,uno,dos", turnos)
	}
}

func TestInstanciaLentaSeCortaConNoDisponible(prueba *testing.T) {
	lenta, _ := instanciaFalsa(prueba, "lenta", 2*time.Second)
	router := routerDePrueba([]*url.URL{lenta}, 100*time.Millisecond)

	inicio := time.Now()
	respuesta := pedir(router, http.MethodGet, "/notificaciones/x", "")

	comprobarNoDisponible(prueba, respuesta)
	if demora := time.Since(inicio); demora > time.Second {
		prueba.Errorf("tardó %v en cortar, se esperaba alrededor de 100 ms", demora)
	}
}

func TestInstanciaCaidaRespondeNoDisponible(prueba *testing.T) {
	// Un servidor que se abre y se cierra deja una dirección donde no atiende nadie.
	servidorCaido := httptest.NewServer(nil)
	direccionCaida, _ := url.Parse(servidorCaido.URL)
	servidorCaido.Close()
	router := routerDePrueba([]*url.URL{direccionCaida}, time.Second)

	comprobarNoDisponible(prueba, pedir(router, http.MethodGet, "/notificaciones/x", ""))
}

func TestHealthDelGateway(prueba *testing.T) {
	instancia, recibidas := instanciaFalsa(prueba, "uno", 0)
	router := routerDePrueba([]*url.URL{instancia}, time.Second)

	if respuesta := pedir(router, http.MethodGet, "/health", ""); respuesta.Code != http.StatusOK {
		prueba.Errorf("estado = %d, se esperaba 200", respuesta.Code)
	}
	if len(*recibidas) != 0 {
		prueba.Error("/health es del gateway: no se reenvía a notificaciones")
	}
}

func comprobarNoDisponible(prueba *testing.T, respuesta *httptest.ResponseRecorder) {
	prueba.Helper()
	if respuesta.Code != http.StatusServiceUnavailable {
		prueba.Fatalf("estado = %d, se esperaba 503 (%s)", respuesta.Code, respuesta.Body.String())
	}
	var cuerpo map[string]string
	if err := json.Unmarshal(respuesta.Body.Bytes(), &cuerpo); err != nil || cuerpo["codigo"] != "no_disponible" || cuerpo["mensaje"] == "" {
		prueba.Errorf("se esperaba {codigo: no_disponible, mensaje: ...}, llegó %s", respuesta.Body.String())
	}
}

func TestPedidosYCapacidadVanAPedidos(prueba *testing.T) {
	notificaciones, recibidasNotificaciones := instanciaFalsa(prueba, "notificaciones", 0)
	pedidos, recibidasPedidos := instanciaFalsa(prueba, "pedidos", 0)
	gin.SetMode(gin.TestMode)
	router := nuevoRouter(
		nuevoProxyBalanceado("notificaciones", []*url.URL{notificaciones}, time.Second, nil),
		nuevoProxyBalanceado("pedidos", []*url.URL{pedidos}, time.Second, nil),
	)

	for _, ruta := range []string{"/pedidos", "/pedidos/id-1/cancelacion", "/capacidad", "/capacidad/2026-10-30"} {
		if respuesta := pedir(router, http.MethodGet, ruta, ""); respuesta.Header().Get("X-Instancia") != "pedidos" {
			prueba.Errorf("%s fue a %q", ruta, respuesta.Header().Get("X-Instancia"))
		}
	}
	if len(*recibidasPedidos) != 4 || len(*recibidasNotificaciones) != 0 {
		prueba.Errorf("pedidos recibió %d y notificaciones %d", len(*recibidasPedidos), len(*recibidasNotificaciones))
	}
}
