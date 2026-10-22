package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/adaptadores/api"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/dominio"
)

// casosFalsos cumple api.CasosDeUso: anota lo que recibe y devuelve lo programado.
type casosFalsos struct {
	grupo, clave string
	solicitud    dominio.Solicitud
	notificacion dominio.Notificacion
	err          error
}

func (casos *casosFalsos) Crear(_ context.Context, grupo, clave string, solicitud dominio.Solicitud) (dominio.Notificacion, error) {
	casos.grupo, casos.clave, casos.solicitud = grupo, clave, solicitud
	return casos.notificacion, casos.err
}

func (casos *casosFalsos) Obtener(_ context.Context, grupo, id string) (dominio.Notificacion, error) {
	casos.grupo = grupo
	if casos.err != nil {
		return dominio.Notificacion{}, casos.err
	}
	if id != casos.notificacion.ID {
		return dominio.Notificacion{}, dominio.ErrNoEncontrada
	}
	return casos.notificacion, nil
}

var momento = time.Date(2026, 10, 21, 14, 30, 0, 0, time.UTC)

func notificacionPendiente() dominio.Notificacion {
	return dominio.Notificacion{
		ID:           "9f1c2a7e-3b4d-4e8a-9c21-5d6f7a8b9c0d",
		Grupo:        "grupo-a",
		Canal:        dominio.CanalEmail,
		Destinatario: "cliente@ejemplo.com",
		Asunto:       "Tu pedido está listo",
		Cuerpo:       "Ya podés retirarlo.",
		Estado:       dominio.EstadoPendiente,
		CreadaEn:     momento,
	}
}

// entorno arma el router con un reloj que se puede adelantar.
type entorno struct {
	router *gin.Engine
	casos  *casosFalsos
	reloj  time.Time
	salud  error
}

func nuevoEntorno(limitePorMinuto int) *entorno {
	gin.SetMode(gin.TestMode)
	actual := &entorno{router: gin.New(), casos: &casosFalsos{notificacion: notificacionPendiente()}, reloj: momento}
	api.Registrar(actual.router, actual.casos, api.Configuracion{
		ClavesAPI:       map[string]string{"clave-a": "grupo-a", "clave-b": "grupo-b"},
		LimitePorMinuto: limitePorMinuto,
		Ahora:           func() time.Time { return actual.reloj },
		ChequearSalud:   func(context.Context) error { return actual.salud },
	})
	return actual
}

const cuerpoValido = `{"canal":"email","destinatario":"cliente@ejemplo.com","asunto":"Tu pedido está listo","cuerpo":"Ya podés retirarlo."}`

func (actual *entorno) pedir(metodo, ruta, cuerpo string, encabezados map[string]string) *httptest.ResponseRecorder {
	pedido := httptest.NewRequest(metodo, ruta, strings.NewReader(cuerpo))
	for nombre, valor := range encabezados {
		pedido.Header.Set(nombre, valor)
	}
	respuesta := httptest.NewRecorder()
	actual.router.ServeHTTP(respuesta, pedido)
	return respuesta
}

func encabezadosPOST() map[string]string {
	return map[string]string{
		"X-API-Key":       "clave-a",
		"Idempotency-Key": "2b0e8f4a-7c1d-4f3e-8a9b-1c2d3e4f5a6b",
		"Content-Type":    "application/json",
	}
}

func leerJSON(prueba *testing.T, respuesta *httptest.ResponseRecorder) map[string]any {
	prueba.Helper()
	var cuerpo map[string]any
	if err := json.Unmarshal(respuesta.Body.Bytes(), &cuerpo); err != nil {
		prueba.Fatalf("la respuesta no es JSON: %v (%s)", err, respuesta.Body.String())
	}
	return cuerpo
}

func comprobarError(prueba *testing.T, respuesta *httptest.ResponseRecorder, estado int, codigo string) {
	prueba.Helper()
	if respuesta.Code != estado {
		prueba.Fatalf("estado = %d, se esperaba %d (%s)", respuesta.Code, estado, respuesta.Body.String())
	}
	cuerpo := leerJSON(prueba, respuesta)
	if cuerpo["codigo"] != codigo || cuerpo["mensaje"] == "" || cuerpo["mensaje"] == nil {
		prueba.Errorf("se esperaba {codigo: %q, mensaje: ...}, llegó %v", codigo, cuerpo)
	}
}

func TestCrearRespondeAceptadaSegunElContrato(prueba *testing.T) {
	actual := nuevoEntorno(60)

	respuesta := actual.pedir(http.MethodPost, "/notificaciones", cuerpoValido, encabezadosPOST())

	if respuesta.Code != http.StatusAccepted {
		prueba.Fatalf("estado = %d, se esperaba 202 (%s)", respuesta.Code, respuesta.Body.String())
	}
	if ubicacion := respuesta.Header().Get("Location"); ubicacion != "/notificaciones/9f1c2a7e-3b4d-4e8a-9c21-5d6f7a8b9c0d" {
		prueba.Errorf("Location = %q", ubicacion)
	}
	cuerpo := leerJSON(prueba, respuesta)
	esperado := map[string]any{
		"id": "9f1c2a7e-3b4d-4e8a-9c21-5d6f7a8b9c0d", "canal": "email", "destinatario": "cliente@ejemplo.com",
		"asunto": "Tu pedido está listo", "estado": "pendiente", "creadaEn": "2026-10-21T14:30:00Z",
	}
	for campo, valor := range esperado {
		if cuerpo[campo] != valor {
			prueba.Errorf("%s = %v, se esperaba %v", campo, cuerpo[campo], valor)
		}
	}
	for _, campo := range []string{"cuerpo", "grupo", "motivo", "enviadaEn"} {
		if _, aparece := cuerpo[campo]; aparece {
			prueba.Errorf("el campo %q no tiene que aparecer en una notificación pendiente", campo)
		}
	}

	if actual.casos.grupo != "grupo-a" || actual.casos.clave != "2b0e8f4a-7c1d-4f3e-8a9b-1c2d3e4f5a6b" {
		prueba.Errorf("llegó grupo %q y clave %q al caso de uso", actual.casos.grupo, actual.casos.clave)
	}
	if actual.casos.solicitud.Cuerpo != "Ya podés retirarlo." || actual.casos.solicitud.Canal != dominio.CanalEmail {
		prueba.Errorf("la solicitud no llegó completa: %+v", actual.casos.solicitud)
	}
}

func TestAutenticacion(prueba *testing.T) {
	actual := nuevoEntorno(60)
	for nombre, clave := range map[string]string{"sin clave": "", "clave desconocida": "otra"} {
		prueba.Run(nombre, func(prueba *testing.T) {
			encabezados := encabezadosPOST()
			encabezados["X-API-Key"] = clave
			comprobarError(prueba, actual.pedir(http.MethodPost, "/notificaciones", cuerpoValido, encabezados), http.StatusUnauthorized, "no_autenticado")
			comprobarError(prueba, actual.pedir(http.MethodGet, "/notificaciones/x", "", map[string]string{"X-API-Key": clave}), http.StatusUnauthorized, "no_autenticado")
		})
	}
}

func TestCrearRechazaPedidosQueNoRespetanElContrato(prueba *testing.T) {
	casos := map[string]struct {
		cuerpo  string
		cambiar func(encabezados map[string]string)
	}{
		"sin Idempotency-Key":  {cuerpoValido, func(encabezados map[string]string) { delete(encabezados, "Idempotency-Key") }},
		"sin Content-Type":     {cuerpoValido, func(encabezados map[string]string) { delete(encabezados, "Content-Type") }},
		"JSON roto":            {`{"canal":`, func(map[string]string) {}},
		"campo desconocido":    {`{"canal":"email","destinatario":"a@b.com","asunto":"x","cuerpo":"y","prioridad":"alta"}`, func(map[string]string) {}},
		"tipo equivocado":      {`{"canal":"email","destinatario":"a@b.com","asunto":5,"cuerpo":"y"}`, func(map[string]string) {}},
		"dos objetos seguidos": {cuerpoValido + cuerpoValido, func(map[string]string) {}},
	}

	for nombre, caso := range casos {
		prueba.Run(nombre, func(prueba *testing.T) {
			actual := nuevoEntorno(60)
			encabezados := encabezadosPOST()
			caso.cambiar(encabezados)

			comprobarError(prueba, actual.pedir(http.MethodPost, "/notificaciones", caso.cuerpo, encabezados), http.StatusBadRequest, "solicitud_invalida")
		})
	}
}

func TestErroresDeLaAplicacionSeTraducenAlContrato(prueba *testing.T) {
	casos := []struct {
		err    error
		estado int
		codigo string
	}{
		{dominio.ErrSolicitudInvalida, http.StatusBadRequest, "solicitud_invalida"},
		{dominio.ErrConflictoIdempotencia, http.StatusConflict, "conflicto_idempotencia"},
		{errors.New("base de datos caída"), http.StatusServiceUnavailable, "no_disponible"},
	}

	for _, caso := range casos {
		prueba.Run(caso.codigo, func(prueba *testing.T) {
			actual := nuevoEntorno(60)
			actual.casos.err = caso.err

			comprobarError(prueba, actual.pedir(http.MethodPost, "/notificaciones", cuerpoValido, encabezadosPOST()), caso.estado, caso.codigo)
		})
	}
}

func TestErrorInternoNoSeLeMuestraAlConsumidor(prueba *testing.T) {
	actual := nuevoEntorno(60)
	actual.casos.err = errors.New("mongodb://usuario:clave@mongo:27017 no responde")

	respuesta := actual.pedir(http.MethodPost, "/notificaciones", cuerpoValido, encabezadosPOST())

	if strings.Contains(respuesta.Body.String(), "mongodb") {
		prueba.Errorf("la respuesta expone detalles internos: %s", respuesta.Body.String())
	}
}

func TestObtener(prueba *testing.T) {
	actual := nuevoEntorno(60)
	enviada := notificacionPendiente()
	enviada.Estado = dominio.EstadoEnviada
	enviada.EnviadaEn = momento.Add(4 * time.Second)
	actual.casos.notificacion = enviada

	respuesta := actual.pedir(http.MethodGet, "/notificaciones/"+enviada.ID, "", map[string]string{"X-API-Key": "clave-b"})

	if respuesta.Code != http.StatusOK {
		prueba.Fatalf("estado = %d, se esperaba 200 (%s)", respuesta.Code, respuesta.Body.String())
	}
	cuerpo := leerJSON(prueba, respuesta)
	if cuerpo["estado"] != "enviada" || cuerpo["enviadaEn"] != "2026-10-21T14:30:04Z" {
		prueba.Errorf("se esperaba enviada con enviadaEn, llegó %v", cuerpo)
	}
	if actual.casos.grupo != "grupo-b" {
		prueba.Errorf("se consultó con el grupo %q, se esperaba el de la clave (grupo-b)", actual.casos.grupo)
	}

	comprobarError(prueba, actual.pedir(http.MethodGet, "/notificaciones/otra", "", map[string]string{"X-API-Key": "clave-b"}), http.StatusNotFound, "no_encontrada")
}

func TestObtenerFallidaMuestraElMotivo(prueba *testing.T) {
	actual := nuevoEntorno(60)
	fallida := notificacionPendiente()
	fallida.Estado = dominio.EstadoFallida
	fallida.Motivo = "El servidor de correo rechazó el destinatario."
	actual.casos.notificacion = fallida

	cuerpo := leerJSON(prueba, actual.pedir(http.MethodGet, "/notificaciones/"+fallida.ID, "", map[string]string{"X-API-Key": "clave-a"}))

	if cuerpo["estado"] != "fallida" || cuerpo["motivo"] != fallida.Motivo {
		prueba.Errorf("se esperaba fallida con motivo, llegó %v", cuerpo)
	}
}

func TestLimiteDeSolicitudesPorMinuto(prueba *testing.T) {
	actual := nuevoEntorno(2)
	get := func(clave string) *httptest.ResponseRecorder {
		return actual.pedir(http.MethodGet, "/notificaciones/9f1c2a7e-3b4d-4e8a-9c21-5d6f7a8b9c0d", "", map[string]string{"X-API-Key": clave})
	}

	get("clave-a")
	get("clave-a")
	actual.reloj = actual.reloj.Add(20 * time.Second)
	respuesta := get("clave-a")

	comprobarError(prueba, respuesta, http.StatusTooManyRequests, "limite_superado")
	if espera := respuesta.Header().Get("Retry-After"); espera != "40" {
		prueba.Errorf("Retry-After = %q, se esperaba 40", espera)
	}
	if otroGrupo := get("clave-b"); otroGrupo.Code != http.StatusOK {
		prueba.Errorf("el límite es por grupo: el grupo b recibió %d", otroGrupo.Code)
	}

	actual.reloj = actual.reloj.Add(40 * time.Second)
	if respuesta := get("clave-a"); respuesta.Code != http.StatusOK {
		prueba.Errorf("pasado el minuto se puede volver a pedir; estado = %d", respuesta.Code)
	}
}

func TestSalud(prueba *testing.T) {
	actual := nuevoEntorno(60)
	if respuesta := actual.pedir(http.MethodGet, "/health", "", nil); respuesta.Code != http.StatusOK {
		prueba.Errorf("estado = %d, se esperaba 200", respuesta.Code)
	}

	actual.salud = errors.New("mongo caído")
	if respuesta := actual.pedir(http.MethodGet, "/health", "", nil); respuesta.Code != http.StatusServiceUnavailable {
		prueba.Errorf("con una dependencia caída se esperaba 503, dio %d", respuesta.Code)
	}
}

func TestVivoNoDependeDeMongoNiDeLaClave(prueba *testing.T) {
	actual := nuevoEntorno(1)
	actual.salud = errors.New("mongo caído")

	// Sin X-API-Key y más veces que el límite de uso (1 por minuto en este entorno).
	for intento := range 3 {
		if respuesta := actual.pedir(http.MethodGet, "/vivo", "", nil); respuesta.Code != http.StatusOK {
			prueba.Fatalf("intento %d: estado = %d, se esperaba 200 aunque MongoDB esté caído", intento+1, respuesta.Code)
		}
	}
	if respuesta := actual.pedir(http.MethodGet, "/health", "", nil); respuesta.Code != http.StatusServiceUnavailable {
		prueba.Errorf("/health tiene que seguir informando la falla de MongoDB; dio %d", respuesta.Code)
	}
}

func TestParsearClavesAPI(prueba *testing.T) {
	claves, err := api.ParsearClavesAPI(" grupo-a:clave1 , grupo-b:clave2,")
	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if claves["clave1"] != "grupo-a" || claves["clave2"] != "grupo-b" || len(claves) != 2 {
		prueba.Errorf("claves = %v", claves)
	}

	for _, invalido := range []string{"", "sin-separador", "grupo-a:", ":clave", "grupo-a:igual,grupo-b:igual"} {
		if _, err := api.ParsearClavesAPI(invalido); err == nil {
			prueba.Errorf("%q tenía que dar error", invalido)
		}
	}
}
