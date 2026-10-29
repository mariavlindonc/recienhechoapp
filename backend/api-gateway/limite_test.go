package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
