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
	router := nuevoRouter([]byte(secretoDePrueba), proxy, http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), medidor.medirSolicitudes())

	for range 4 {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/notificaciones/x", nil))
	}

	// Después de la primera falla, la caída sale del balanceo y el resto va a la sana.
	if cantidad := testutil.ToFloat64(medidor.reenvios.WithLabelValues("notificaciones", uno.Host, "202")); cantidad != 3 {
		prueba.Errorf("la instancia sana recibió %v reenvíos con 202, se esperaban 3", cantidad)
	}
	if cantidad := testutil.ToFloat64(medidor.reenvios.WithLabelValues("notificaciones", caida.Host, "503")); cantidad != 1 {
		prueba.Errorf("la instancia caída tiene %v reenvíos con 503, se esperaba 1", cantidad)
	}
	if cantidad := testutil.ToFloat64(medidor.solicitudes.WithLabelValues("GET", "/notificaciones/*resto", "503")); cantidad != 1 {
		prueba.Errorf("el gateway contó %v solicitudes con 503, se esperaba 1", cantidad)
	}
}

func TestLaSaludDeCadaInstanciaQuedaEnLaMetrica(prueba *testing.T) {
	uno, _ := instanciaFalsa(prueba, "uno", 0)
	dos, _ := instanciaFalsa(prueba, "dos", 0)
	medidor := nuevasMetricas()
	proxy := nuevoProxyBalanceado("notificaciones", []*url.URL{uno, dos}, time.Second, nil)
	proxy.alCambiarSalud = medidor.marcarSalud("notificaciones")

	for range fallasParaSacar {
		proxy.registrarChequeo(1, errChequeo)
	}

	if valor := testutil.ToFloat64(medidor.sanas.WithLabelValues("notificaciones", dos.Host)); valor != 0 {
		prueba.Errorf("gateway_instancia_sana de la caída = %v, se esperaba 0", valor)
	}
	proxy.registrarChequeo(1, nil)
	if valor := testutil.ToFloat64(medidor.sanas.WithLabelValues("notificaciones", dos.Host)); valor != 1 {
		prueba.Errorf("gateway_instancia_sana después del OK = %v, se esperaba 1", valor)
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
