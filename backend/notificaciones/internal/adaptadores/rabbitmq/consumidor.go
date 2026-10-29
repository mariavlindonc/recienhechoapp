// Package rabbitmq consume los eventos de pedidos (ADR-003) y los convierte en
// notificaciones para el comprador. Es otra entrada a la aplicación, como la API.
package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/dominio"
)

// Topología de ADR-003. El exchange lo declara también pedidos; declararlo dos
// veces con los mismos datos no hace nada.
const (
	exchangePedidos  = "pedidos"
	exchangeFallidos = "notificaciones.fallidos"
	Cola             = "notificaciones.pedidos"
	ColaFallidos     = "notificaciones.fallidos"
	claveDeRuteo     = "pedido.estado_cambiado"

	// reintentosMaximos es cuántas veces se vuelve a entregar un mensaje que falló
	// antes de mandarlo a la cola de fallidos.
	reintentosMaximos = 3

	// GrupoPropio es el grupo con el que se guardan las notificaciones de los
	// pedidos propios, separado de los grupos que usan la capacidad publicada.
	GrupoPropio = "recienhecho"

	esperaReconexion = 5 * time.Second
)

// CasosDeUso es lo que este adaptador necesita de la aplicación.
type CasosDeUso interface {
	Crear(contexto context.Context, grupo, clave string, solicitud dominio.Solicitud) (dominio.Notificacion, error)
}

// evento es el formato de ADR-003.
type evento struct {
	IDEvento string `json:"idEvento"`
	Tipo     string `json:"tipo"`
	Version  int    `json:"version"`
	Datos    struct {
		IDPedido       string `json:"idPedido"`
		EmailComprador string `json:"emailComprador"`
		EstadoNuevo    string `json:"estadoNuevo"`
		Items          []struct {
			NombreProducto string `json:"nombreProducto"`
			Cantidad       int    `json:"cantidad"`
		} `json:"items"`
		Fecha         string `json:"fecha"`
		Motivo        string `json:"motivo"`
		TotalCentavos int64  `json:"totalCentavos"`
	} `json:"datos"`
}

// errNoProcesable marca un mensaje que nunca se va a poder procesar: va directo a
// la cola de fallidos, sin reintentos.
var errNoProcesable = errors.New("mensaje no procesable")

// Consumir escucha la cola hasta que se cancela el contexto. Si RabbitMQ se cae,
// reintenta la conexión cada pocos segundos: mientras tanto el resto del servicio
// sigue atendiendo.
func Consumir(contexto context.Context, direccion string, casos CasosDeUso) {
	for {
		err := consumirUnaConexion(contexto, direccion, casos)
		if contexto.Err() != nil {
			return
		}
		slog.Warn("sin conexión con RabbitMQ; se reintenta", "error", err, "espera", esperaReconexion.String())
		select {
		case <-contexto.Done():
			return
		case <-time.After(esperaReconexion):
		}
	}
}

func consumirUnaConexion(contexto context.Context, direccion string, casos CasosDeUso) error {
	conexion, err := amqp.DialConfig(direccion, amqp.Config{Dial: amqp.DefaultDial(2 * time.Second)})
	if err != nil {
		return fmt.Errorf("conectar a RabbitMQ: %w", err)
	}
	defer conexion.Close()
	canal, err := conexion.Channel()
	if err != nil {
		return fmt.Errorf("abrir canal: %w", err)
	}
	if err := declarar(canal); err != nil {
		return err
	}
	mensajes, err := canal.ConsumeWithContext(contexto, Cola, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consumir %s: %w", Cola, err)
	}
	slog.Info("consumiendo eventos de pedidos", "cola", Cola)
	for mensaje := range mensajes {
		procesar(contexto, casos, mensaje)
	}
	return errors.New("se cerró el canal de RabbitMQ")
}

// declarar arma la cola con su cola de fallidos. Es una cola quorum: RabbitMQ
// cuenta las entregas y, pasado el límite, manda el mensaje a la de fallidos.
func declarar(canal *amqp.Channel) error {
	pasos := []func() error{
		func() error { return canal.Qos(10, 0, false) },
		func() error {
			return canal.ExchangeDeclare(exchangePedidos, amqp.ExchangeTopic, true, false, false, false, nil)
		},
		func() error {
			return canal.ExchangeDeclare(exchangeFallidos, amqp.ExchangeFanout, true, false, false, false, nil)
		},
		func() error {
			_, err := canal.QueueDeclare(ColaFallidos, true, false, false, false, amqp.Table{"x-queue-type": "quorum"})
			return err
		},
		func() error { return canal.QueueBind(ColaFallidos, "", exchangeFallidos, false, nil) },
		func() error {
			_, err := canal.QueueDeclare(Cola, true, false, false, false, amqp.Table{
				"x-queue-type":           "quorum",
				"x-delivery-limit":       reintentosMaximos,
				"x-dead-letter-exchange": exchangeFallidos,
			})
			return err
		},
		func() error { return canal.QueueBind(Cola, claveDeRuteo, exchangePedidos, false, nil) },
	}
	for _, paso := range pasos {
		if err := paso(); err != nil {
			return fmt.Errorf("declarar la topología de RabbitMQ: %w", err)
		}
	}
	return nil
}

// receptor es lo que procesar necesita del mensaje; *amqp.Delivery lo cumple.
type receptor interface {
	Ack(multiple bool) error
	Nack(multiple, requeue bool) error
}

func procesar(contexto context.Context, casos CasosDeUso, mensaje amqp.Delivery) {
	contexto = otel.GetTextMapPropagator().Extract(contexto, portadorDe(mensaje.Headers))
	contexto, span := otel.Tracer("recienhecho/notificaciones/rabbitmq").Start(contexto, "consumir "+claveDeRuteo, trace.WithSpanKind(trace.SpanKindConsumer))
	defer span.End()
	err := Procesar(contexto, casos, mensaje.Body)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	responder(contexto, &mensaje, err)
}

// responder confirma el mensaje o lo devuelve. Uno no procesable va directo a la
// cola de fallidos; uno que falló por algo transitorio se vuelve a entregar.
func responder(contexto context.Context, mensaje receptor, err error) {
	switch {
	case err == nil:
		mensaje.Ack(false)
	case errors.Is(err, errNoProcesable):
		slog.ErrorContext(contexto, "evento no procesable; va a la cola de fallidos", "error", err)
		mensaje.Nack(false, false)
	default:
		slog.WarnContext(contexto, "no se pudo procesar un evento; se reintenta", "error", err)
		mensaje.Nack(false, true)
	}
}

// Procesar convierte el evento en una notificación. El idEvento es la clave de
// idempotencia: si el broker entrega el mismo evento dos veces, se crea una sola
// notificación y sale un solo mail (C9).
func Procesar(contexto context.Context, casos CasosDeUso, cuerpo []byte) error {
	var recibido evento
	if err := json.Unmarshal(cuerpo, &recibido); err != nil {
		return fmt.Errorf("%w: JSON inválido: %v", errNoProcesable, err)
	}
	if recibido.Tipo != claveDeRuteo || recibido.Version != 1 || recibido.IDEvento == "" {
		return fmt.Errorf("%w: evento %q versión %d sin soporte", errNoProcesable, recibido.Tipo, recibido.Version)
	}
	asunto, cuerpoMail := mensajeDe(recibido)
	_, err := casos.Crear(contexto, GrupoPropio, recibido.IDEvento, dominio.Solicitud{
		Canal:        dominio.CanalEmail,
		Destinatario: recibido.Datos.EmailComprador,
		Asunto:       asunto,
		Cuerpo:       cuerpoMail,
	})
	if errors.Is(err, dominio.ErrSolicitudInvalida) || errors.Is(err, dominio.ErrConflictoIdempotencia) {
		return fmt.Errorf("%w: %v", errNoProcesable, err)
	}
	return err
}

// mensajeDe arma el asunto y el cuerpo del mail según el estado nuevo del pedido.
func mensajeDe(recibido evento) (string, string) {
	datos := recibido.Datos
	productos := make([]string, len(datos.Items))
	for posicion, item := range datos.Items {
		productos[posicion] = fmt.Sprintf("%d × %s", item.Cantidad, item.NombreProducto)
	}
	pedido := fmt.Sprintf("tu pedido (%s) para el %s", strings.Join(productos, ", "), datos.Fecha)
	switch datos.EstadoNuevo {
	// Pedidos especiales (C6, ADR-017).
	case "solicitado":
		return "Recibimos tu pedido especial", fmt.Sprintf("Recibimos %s. Te avisamos cuando lo coticemos.", pedido)
	case "cotizado":
		return "Cotizamos tu pedido especial", fmt.Sprintf("El precio de %s es $%d,%02d. Tenés 48 horas para aceptarlo.", pedido, datos.TotalCentavos/100, datos.TotalCentavos%100)
	case "rechazado":
		return "Rechazaste la cotización", fmt.Sprintf("Registramos que no aceptaste la cotización de %s.", pedido)
	case "vencido":
		return "La cotización venció", fmt.Sprintf("Pasaron más de 48 horas y venció la cotización de %s.", pedido)
	case "pendiente_confirmacion":
		return "Recibimos tu pedido", fmt.Sprintf("Recibimos %s. Te avisamos cuando esté confirmado.", pedido)
	case "confirmado":
		return "Tu pedido está confirmado", fmt.Sprintf("Confirmamos %s.", pedido)
	case "en_produccion":
		return "Estamos preparando tu pedido", fmt.Sprintf("Empezamos a preparar %s.", pedido)
	case "listo":
		return "Tu pedido está listo", fmt.Sprintf("Ya está listo %s.", pedido)
	case "entregado":
		return "Entregamos tu pedido", fmt.Sprintf("Entregamos %s. ¡Que lo disfrutes!", pedido)
	case "cancelado":
		cuerpo := fmt.Sprintf("Se canceló %s.", pedido)
		if datos.Motivo != "" {
			cuerpo += " " + datos.Motivo
		}
		return "Tu pedido fue cancelado", cuerpo
	}
	return "Tu pedido cambió de estado", fmt.Sprintf("El estado de %s ahora es %s.", pedido, datos.EstadoNuevo)
}

// portadorDe lee el traceparent de los headers del mensaje, para continuar la
// traza que empezó en pedidos (ADR-006).
func portadorDe(encabezados amqp.Table) propagation.MapCarrier {
	portador := propagation.MapCarrier{}
	for clave, valor := range encabezados {
		if texto, esTexto := valor.(string); esTexto {
			portador[clave] = texto
		}
	}
	return portador
}
