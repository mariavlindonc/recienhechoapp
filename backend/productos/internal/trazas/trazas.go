// Package trazas configura OpenTelemetry (ADR-006): exporta los spans por OTLP al
// destino de OTEL_EXPORTER_OTLP_ENDPOINT (Jaeger) y propaga la traza entre
// servicios con el header traceparent (W3C).
package trazas

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Iniciar configura el proveedor de trazas global y devuelve la función que lo
// cierra (manda los spans que queden pendientes). Si OTEL_EXPORTER_OTLP_ENDPOINT
// no está definida, no exporta nada pero igual propaga la traza recibida.
func Iniciar(contexto context.Context, servicio string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	// Si Jaeger no responde, los spans se pierden pero el servicio sigue: el error
	// queda en el log en vez de interrumpir algo.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Warn("no se pudieron exportar las trazas", "error", err)
	}))

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		slog.Info("trazas sin exportar: falta OTEL_EXPORTER_OTLP_ENDPOINT")
		return func(context.Context) error { return nil }, nil
	}

	exportador, err := otlptracehttp.New(contexto)
	if err != nil {
		return nil, fmt.Errorf("crear el exportador de trazas: %w", err)
	}
	recurso, err := resource.Merge(resource.Default(), resource.NewSchemaless(attribute.String("service.name", servicio)))
	if err != nil {
		return nil, fmt.Errorf("describir el servicio para las trazas: %w", err)
	}
	proveedor := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exportador), sdktrace.WithResource(recurso))
	otel.SetTracerProvider(proveedor)
	return proveedor.Shutdown, nil
}
