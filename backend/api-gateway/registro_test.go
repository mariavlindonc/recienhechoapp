package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestMiddlewareRegistraCadaSolicitudEnJSON(prueba *testing.T) {
	instancia, _ := instanciaFalsa(prueba, "uno", 0)
	var salida bytes.Buffer
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(registrarSolicitudes(nuevoLogger(&salida, nombreServicio)))
	router.Any("/notificaciones/*resto", gin.WrapH(nuevoProxyBalanceado("notificaciones", []*url.URL{instancia}, time.Second, nil)))

	pedido := httptest.NewRequest(http.MethodGet, "/notificaciones/9f1c2a7e", nil)
	pedido.Header.Set("X-API-Key", "clave-secreta")
	router.ServeHTTP(httptest.NewRecorder(), pedido)

	var linea map[string]any
	if err := json.Unmarshal(salida.Bytes(), &linea); err != nil {
		prueba.Fatalf("la línea no es JSON: %q (%v)", salida.String(), err)
	}
	esperado := map[string]any{"servicio": "api-gateway", "metodo": "GET", "ruta": "/notificaciones/*resto", "estado": float64(202)}
	for campo, valor := range esperado {
		if linea[campo] != valor {
			prueba.Errorf("%s = %v, se esperaba %v", campo, linea[campo], valor)
		}
	}
	if _, tiene := linea["duracion_ms"]; !tiene {
		prueba.Error("falta duracion_ms")
	}
	if strings.Contains(salida.String(), "clave-secreta") {
		prueba.Errorf("el log no tiene que incluir la clave de API: %s", salida.String())
	}
}
