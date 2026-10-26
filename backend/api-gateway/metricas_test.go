package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

func TestSeCuentaCadaReenvioPorInstanciaYEstado(prueba *testing.T) {
	uno, _ := instanciaFalsa(prueba, "uno", 0)
	servidorCaido := httptest.NewServer(nil)
	caida, _ := url.Parse(servidorCaido.URL)
	servidorCaido.Close()
	medidor := nuevasMetricas()
	proxy := nuevoProxyBalanceado("notificaciones", []*url.URL{uno, caida}, time.Second, medidor.contarReenvio("notificaciones"))
	router := nuevoRouter(proxy, http.NotFoundHandler(), medidor.medirSolicitudes())

	for range 4 {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/notificaciones/x", nil))
	}

	if cantidad := testutil.ToFloat64(medidor.reenvios.WithLabelValues("notificaciones", uno.Host, "202")); cantidad != 2 {
		prueba.Errorf("la instancia sana recibió %v reenvíos con 202, se esperaban 2", cantidad)
	}
	if cantidad := testutil.ToFloat64(medidor.reenvios.WithLabelValues("notificaciones", caida.Host, "503")); cantidad != 2 {
		prueba.Errorf("la instancia caída tiene %v reenvíos con 503, se esperaban 2", cantidad)
	}
	if cantidad := testutil.ToFloat64(medidor.solicitudes.WithLabelValues("GET", "/notificaciones/*resto", "503")); cantidad != 2 {
		prueba.Errorf("el gateway contó %v solicitudes con 503, se esperaban 2", cantidad)
	}
}

func TestLasMetricasUsanElRegistroPropio(prueba *testing.T) {
	medidor := nuevasMetricas()
	cantidad, err := testutil.GatherAndCount(medidor.registro, "gateway_solicitudes_total", "solicitudes_http_total")
	if err != nil {
		prueba.Fatalf("leer el registro: %v", err)
	}
	if cantidad != 0 {
		prueba.Errorf("sin tráfico no tiene que haber series, hubo %d", cantidad)
	}
	if nombres, _ := medidor.registro.Gather(); !strings.Contains(nombresDe(nombres), "go_goroutines") {
		prueba.Error("faltan las métricas del runtime de Go")
	}
}

func nombresDe(familias []*dto.MetricFamily) string {
	var nombres []string
	for _, familia := range familias {
		nombres = append(nombres, familia.GetName())
	}
	return strings.Join(nombres, ",")
}
