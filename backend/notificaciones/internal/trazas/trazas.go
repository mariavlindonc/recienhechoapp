// Package trazas configura OpenTelemetry (ADR-006): exporta los spans por OTLP al
// destino de OTEL_EXPORTER_OTLP_ENDPOINT (Jaeger) y propaga la traza entre
// servicios con el header traceparent (W3C).
package trazas

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"go.mongodb.org/mongo-driver/v2/event"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
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

// MonitorMongo crea un span por cada comando que se manda a MongoDB, pero solo si
// el comando es parte de una solicitud que ya tiene una traza. Así el proceso de
// envío, que consulta MongoDB cada pocos segundos, no llena Jaeger de trazas sueltas.
//
// Se escribe acá porque la instrumentación oficial (otelmongo) todavía no tiene
// una versión publicada para mongo-driver/v2.
func MonitorMongo() *event.CommandMonitor {
	trazador := otel.Tracer("recienhecho/notificaciones/mongodb")
	var abiertos sync.Map // RequestID del comando -> span abierto

	terminar := func(idPedido int64, err error) {
		guardado, existe := abiertos.LoadAndDelete(idPedido)
		if !existe {
			return
		}
		span := guardado.(trace.Span)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}

	return &event.CommandMonitor{
		Started: func(contexto context.Context, comando *event.CommandStartedEvent) {
			if !trace.SpanContextFromContext(contexto).IsValid() {
				return
			}
			_, span := trazador.Start(contexto, "mongodb "+comando.CommandName,
				trace.WithSpanKind(trace.SpanKindClient),
				trace.WithAttributes(
					attribute.String("db.system.name", "mongodb"),
					attribute.String("db.namespace", comando.DatabaseName),
					attribute.String("db.operation.name", comando.CommandName),
				),
			)
			abiertos.Store(comando.RequestID, span)
		},
		Succeeded: func(_ context.Context, resultado *event.CommandSucceededEvent) {
			terminar(resultado.RequestID, nil)
		},
		Failed: func(_ context.Context, falla *event.CommandFailedEvent) {
			terminar(falla.RequestID, falla.Failure)
		},
	}
}
