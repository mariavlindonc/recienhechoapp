package trazas_test

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/v2/event"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/trazas"
)

// grabarSpans hace que los spans queden en memoria para revisarlos.
func grabarSpans(prueba *testing.T) (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	prueba.Helper()
	grabador := tracetest.NewSpanRecorder()
	proveedor := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(grabador))
	anterior := otel.GetTracerProvider()
	otel.SetTracerProvider(proveedor)
	prueba.Cleanup(func() { otel.SetTracerProvider(anterior) })
	return grabador, proveedor
}

func comando(idPedido int64) *event.CommandStartedEvent {
	return &event.CommandStartedEvent{CommandName: "insert", DatabaseName: "notificaciones", RequestID: idPedido}
}

func TestMonitorMongoCreaUnSpanDentroDeUnaSolicitud(prueba *testing.T) {
	grabador, proveedor := grabarSpans(prueba)
	monitor := trazas.MonitorMongo()
	contexto, solicitud := proveedor.Tracer("prueba").Start(context.Background(), "POST /notificaciones")

	monitor.Started(contexto, comando(1))
	monitor.Succeeded(contexto, &event.CommandSucceededEvent{CommandFinishedEvent: event.CommandFinishedEvent{RequestID: 1}})
	solicitud.End()

	spans := grabador.Ended()
	if len(spans) != 2 {
		prueba.Fatalf("se esperaban 2 spans (solicitud y comando), hubo %d", len(spans))
	}
	mongo := spans[0]
	if mongo.Name() != "mongodb insert" {
		prueba.Errorf("nombre = %q", mongo.Name())
	}
	if mongo.Parent().SpanID() != solicitud.SpanContext().SpanID() {
		prueba.Error("el span de MongoDB tiene que colgar de la solicitud")
	}
}

func TestMonitorMongoNoTrazaComandosSueltos(prueba *testing.T) {
	grabador, _ := grabarSpans(prueba)
	monitor := trazas.MonitorMongo()

	// Así consulta el proceso de envío: sin una solicitud en curso.
	monitor.Started(context.Background(), comando(1))
	monitor.Succeeded(context.Background(), &event.CommandSucceededEvent{CommandFinishedEvent: event.CommandFinishedEvent{RequestID: 1}})

	if cantidad := len(grabador.Ended()); cantidad != 0 {
		prueba.Errorf("sin traza en curso no se crean spans; hubo %d", cantidad)
	}
}

func TestMonitorMongoMarcaLosComandosFallidos(prueba *testing.T) {
	grabador, proveedor := grabarSpans(prueba)
	monitor := trazas.MonitorMongo()
	contexto, solicitud := proveedor.Tracer("prueba").Start(context.Background(), "GET /notificaciones/:id")
	defer solicitud.End()

	monitor.Started(contexto, comando(7))
	monitor.Failed(contexto, &event.CommandFailedEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{RequestID: 7},
		Failure:              errors.New("tiempo agotado"),
	})

	spans := grabador.Ended()
	if len(spans) != 1 || spans[0].Status().Code != codes.Error {
		prueba.Errorf("se esperaba un span con error, hubo %v", spans)
	}
}

func TestIniciarSinDestinoNoFalla(prueba *testing.T) {
	prueba.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	apagar, err := trazas.Iniciar(context.Background(), "notificaciones")

	if err != nil {
		prueba.Fatalf("sin destino no tiene que fallar: %v", err)
	}
	if err := apagar(context.Background()); err != nil {
		prueba.Errorf("apagar: %v", err)
	}
}
