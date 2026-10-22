package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestElReenvioContinuaLaMismaTraza(prueba *testing.T) {
	grabador := tracetest.NewSpanRecorder()
	proveedorAnterior, propagadorAnterior := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(grabador)))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	prueba.Cleanup(func() {
		otel.SetTracerProvider(proveedorAnterior)
		otel.SetTextMapPropagator(propagadorAnterior)
	})
	instancia, recibidas := instanciaFalsa(prueba, "uno", 0)
	router := routerDePrueba([]*url.URL{instancia}, time.Second)

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/notificaciones/x", nil))

	// traceparent: version-idDeTraza-idDeSpan-opciones (W3C).
	partes := strings.Split((*recibidas)[0].Header.Get("traceparent"), "-")
	if len(partes) != 4 {
		prueba.Fatalf("notificaciones no recibió un traceparent válido: %q", (*recibidas)[0].Header.Get("traceparent"))
	}
	spans := grabador.Ended()
	if len(spans) < 2 {
		prueba.Fatalf("se esperaban al menos 2 spans en el gateway (entrada y reenvío), hubo %d", len(spans))
	}
	for _, span := range spans {
		if span.SpanContext().TraceID().String() != partes[1] {
			prueba.Errorf("el span %q es de otra traza: %s, se esperaba %s", span.Name(), span.SpanContext().TraceID(), partes[1])
		}
	}
}

func TestHealthNoSeTraza(prueba *testing.T) {
	grabador := tracetest.NewSpanRecorder()
	anterior := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(grabador)))
	prueba.Cleanup(func() { otel.SetTracerProvider(anterior) })
	instancia, _ := instanciaFalsa(prueba, "uno", 0)
	router := routerDePrueba([]*url.URL{instancia}, time.Second)

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	if cantidad := len(grabador.Ended()); cantidad != 0 {
		prueba.Errorf("/health no se traza; hubo %d spans", cantidad)
	}
}
