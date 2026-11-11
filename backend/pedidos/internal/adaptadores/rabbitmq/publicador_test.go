package rabbitmq_test

// Tests de integración contra un RabbitMQ real. Corren solo si está definida
// RABBITMQ_URI_TEST. Usan el exchange "pedidos", así que el broker no puede tener
// la cola de notificaciones (usar uno descartable, no el del compose), por ejemplo:
//
//	docker run -d --rm --name rabbitmq-test -p 127.0.0.1:5673:5672 rabbitmq:4.3.5-alpine
//	RABBITMQ_URI_TEST="amqp://guest:guest@127.0.0.1:5673/" go test ./...

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/adaptadores/rabbitmq"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

func direccionDePrueba(prueba *testing.T) string {
	prueba.Helper()
	direccion := os.Getenv("RABBITMQ_URI_TEST")
	if direccion == "" {
		prueba.Skip("RABBITMQ_URI_TEST no está definida: se saltea el test de integración")
	}
	return direccion
}

func eventoDePrueba(id string) puertos.EventoGuardado {
	return puertos.EventoGuardado{
		Evento: dominio.Evento{
			ID:         id,
			Tipo:       dominio.TipoEstadoCambiado,
			Version:    1,
			OcurridoEn: time.Now(),
			Datos:      dominio.DatosEstadoCambiado{IDPedido: "pedido-1", EstadoNuevo: "cancelado", MensajeVendedor: "No llegamos para esa fecha."},
		},
		Traza: map[string]string{"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"},
	}
}

// Sin una cola enlazada, RabbitMQ descartaría el mensaje: no puede contar como
// publicado, porque el evento se borraría de la bandeja de salida y se perdería.
func TestSinColaNoSeDaPorPublicado(prueba *testing.T) {
	publicador := rabbitmq.NuevoPublicador(direccionDePrueba(prueba))
	defer publicador.Cerrar()

	if err := publicador.Publicar(context.Background(), eventoDePrueba("evento-sin-cola")); !errors.Is(err, rabbitmq.ErrSinCola) {
		prueba.Fatalf("sin cola el broker descarta el mensaje: se esperaba ErrSinCola, dio: %v", err)
	}

	// Cuando la cola aparece, el reintento del mismo evento sale.
	prepararCola(prueba, direccionDePrueba(prueba))
	if err := publicador.Publicar(context.Background(), eventoDePrueba("evento-sin-cola")); err != nil {
		prueba.Errorf("con la cola creada tenía que publicarse: %v", err)
	}
}

// prepararCola crea una cola enlazada al exchange de pedidos. Es exclusiva: se
// borra sola al terminar el test, cuando se cierra la conexión.
func prepararCola(prueba *testing.T, direccion string) (*amqp.Channel, string) {
	prueba.Helper()
	conexion, err := amqp.Dial(direccion)
	if err != nil {
		prueba.Fatalf("conectar: %v", err)
	}
	prueba.Cleanup(func() { conexion.Close() })
	canal, err := conexion.Channel()
	if err == nil {
		err = canal.ExchangeDeclare(rabbitmq.Exchange, amqp.ExchangeTopic, true, false, false, false, nil)
	}
	if err != nil {
		prueba.Fatalf("preparar exchange: %v", err)
	}
	cola, err := canal.QueueDeclare("", false, true, true, false, nil)
	if err == nil {
		err = canal.QueueBind(cola.Name, dominio.TipoEstadoCambiado, rabbitmq.Exchange, false, nil)
	}
	if err != nil {
		prueba.Fatalf("preparar cola: %v", err)
	}
	return canal, cola.Name
}

func TestPublicaElEventoConSuTraza(prueba *testing.T) {
	direccion := direccionDePrueba(prueba)
	canal, cola := prepararCola(prueba, direccion)
	publicador := rabbitmq.NuevoPublicador(direccion)
	defer publicador.Cerrar()

	if err := publicador.Publicar(context.Background(), eventoDePrueba("evento-1")); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}

	mensaje, encontrado, err := canal.Get(cola, true)
	if err != nil || !encontrado {
		prueba.Fatalf("el mensaje tenía que estar en la cola: %v", err)
	}
	if !strings.Contains(string(mensaje.Body), `"mensajeVendedor":"No llegamos para esa fecha."`) {
		prueba.Errorf("el cuerpo no lleva el mensaje del vendedor: %s", mensaje.Body)
	}
	if mensaje.MessageId != "evento-1" || mensaje.Headers["traceparent"] == nil || mensaje.DeliveryMode != amqp.Persistent {
		prueba.Errorf("mensaje %s, headers %v, modo %d", mensaje.MessageId, mensaje.Headers, mensaje.DeliveryMode)
	}
}
