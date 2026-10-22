package registro_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/registro"
)

// leerLineas separa la salida en líneas y verifica que cada una sea JSON.
func leerLineas(prueba *testing.T, salida *bytes.Buffer) []map[string]any {
	prueba.Helper()
	var lineas []map[string]any
	for _, texto := range strings.Split(strings.TrimSpace(salida.String()), "\n") {
		var linea map[string]any
		if err := json.Unmarshal([]byte(texto), &linea); err != nil {
			prueba.Fatalf("la línea no es JSON: %q (%v)", texto, err)
		}
		lineas = append(lineas, linea)
	}
	return lineas
}

func contextoConTraza() context.Context {
	traza := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36},
		SpanID:     trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
		TraceFlags: trace.FlagsSampled,
	})
	return trace.ContextWithSpanContext(context.Background(), traza)
}

func TestLoggerEscribeJSONConElServicio(prueba *testing.T) {
	var salida bytes.Buffer
	logger := registro.NuevoLogger(&salida, "notificaciones")

	logger.Info("arrancó", "puerto", "8084")

	linea := leerLineas(prueba, &salida)[0]
	if linea["servicio"] != "notificaciones" || linea["msg"] != "arrancó" || linea["puerto"] != "8084" {
		prueba.Errorf("línea = %v", linea)
	}
	if _, tiene := linea["trace_id"]; tiene {
		prueba.Error("sin traza activa no tiene que aparecer trace_id")
	}
}

func TestLoggerAgregaLaTrazaDelContexto(prueba *testing.T) {
	var salida bytes.Buffer
	logger := registro.NuevoLogger(&salida, "notificaciones").With("componente", "envio")

	logger.InfoContext(contextoConTraza(), "algo pasó")

	linea := leerLineas(prueba, &salida)[0]
	if linea["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" || linea["span_id"] != "00f067aa0ba902b7" {
		prueba.Errorf("faltan trace_id o span_id: %v", linea)
	}
	if linea["componente"] != "envio" {
		prueba.Error("los atributos agregados con With se tienen que mantener")
	}
}

func TestMiddlewareRegistraCadaSolicitud(prueba *testing.T) {
	gin.SetMode(gin.TestMode)
	var salida bytes.Buffer
	router := gin.New()
	router.Use(registro.Solicitudes(registro.NuevoLogger(&salida, "notificaciones")))
	router.GET("/notificaciones/:id", func(contexto *gin.Context) { contexto.Status(http.StatusNotFound) })

	pedido := httptest.NewRequest(http.MethodGet, "/notificaciones/9f1c2a7e", nil)
	pedido.Header.Set("X-API-Key", "clave-secreta")
	router.ServeHTTP(httptest.NewRecorder(), pedido)

	linea := leerLineas(prueba, &salida)[0]
	esperado := map[string]any{"servicio": "notificaciones", "metodo": "GET", "ruta": "/notificaciones/:id", "estado": float64(404)}
	for campo, valor := range esperado {
		if linea[campo] != valor {
			prueba.Errorf("%s = %v, se esperaba %v", campo, linea[campo], valor)
		}
	}
	if _, tiene := linea["duracion_ms"]; !tiene {
		prueba.Error("falta duracion_ms")
	}
	if strings.Contains(salida.String(), "clave-secreta") || strings.Contains(salida.String(), "9f1c2a7e") {
		prueba.Errorf("el log no tiene que incluir la clave de API ni el id: %s", salida.String())
	}
}

func TestMiddlewareConRutaInexistente(prueba *testing.T) {
	gin.SetMode(gin.TestMode)
	var salida bytes.Buffer
	router := gin.New()
	router.Use(registro.Solicitudes(registro.NuevoLogger(&salida, "notificaciones")))

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/no-existe", nil))

	if linea := leerLineas(prueba, &salida)[0]; linea["ruta"] != "sin_ruta" || linea["estado"] != float64(404) {
		prueba.Errorf("línea = %v", linea)
	}
}
