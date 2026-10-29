// Package rabbitmq implementa el puerto Publicador sobre RabbitMQ (ADR-003): manda
// los eventos al exchange "pedidos" y espera la confirmación del broker.
package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

// Exchange es donde pedidos publica sus eventos; cada consumidor arma su cola.
const Exchange = "pedidos"

// esperaPorPublicacion es lo máximo que se espera la confirmación del broker.
const esperaPorPublicacion = 5 * time.Second

// Publicador cumple puertos.Publicador. Se conecta recién cuando tiene algo para
// publicar y se reconecta si la conexión se cae: con RabbitMQ caído el servicio
// sigue funcionando y los eventos esperan en la bandeja de salida (ADR-011).
type Publicador struct {
	direccion string

	mutex    sync.Mutex
	conexion *amqp.Connection
	canal    *amqp.Channel
}

var _ puertos.Publicador = (*Publicador)(nil)

// NuevoPublicador no se conecta todavía.
func NuevoPublicador(direccion string) *Publicador {
	return &Publicador{direccion: direccion}
}

// cuerpoEvento es el formato del evento definido en ADR-003.
type cuerpoEvento struct {
	IDEvento   string      `json:"idEvento"`
	Tipo       string      `json:"tipo"`
	Version    int         `json:"version"`
	OcurridoEn time.Time   `json:"ocurridoEn"`
	Datos      cuerpoDatos `json:"datos"`
}

type cuerpoDatos struct {
	IDPedido       string       `json:"idPedido"`
	IDComprador    string       `json:"idComprador"`
	EmailComprador string       `json:"emailComprador"`
	EstadoAnterior string       `json:"estadoAnterior,omitempty"`
	EstadoNuevo    string       `json:"estadoNuevo"`
	Items          []cuerpoItem `json:"items"`
	Fecha          string       `json:"fecha"`
	Motivo         string       `json:"motivo,omitempty"`
	TotalCentavos  int64        `json:"totalCentavos,omitempty"`
}

type cuerpoItem struct {
	NombreProducto string `json:"nombreProducto"`
	Cantidad       int    `json:"cantidad"`
}

// Publicar manda el evento como mensaje persistente y devuelve nil solo si el
// broker confirmó que lo guardó.
func (publicador *Publicador) Publicar(contexto context.Context, guardado puertos.EventoGuardado) error {
	evento := guardado.Evento
	datos := evento.Datos
	items := make([]cuerpoItem, len(datos.Items))
	for indice, item := range datos.Items {
		items[indice] = cuerpoItem(item)
	}
	cuerpo, err := json.Marshal(cuerpoEvento{
		IDEvento:   evento.ID,
		Tipo:       evento.Tipo,
		Version:    evento.Version,
		OcurridoEn: evento.OcurridoEn,
		Datos: cuerpoDatos{
			IDPedido:       datos.IDPedido,
			IDComprador:    datos.IDComprador,
			EmailComprador: datos.EmailComprador,
			EstadoAnterior: datos.EstadoAnterior,
			EstadoNuevo:    datos.EstadoNuevo,
			Items:          items,
			Fecha:          datos.Fecha,
			Motivo:         datos.Motivo,
			TotalCentavos:  datos.TotalCentavos,
		},
	})
	if err != nil {
		return fmt.Errorf("armar el evento %s: %w", evento.ID, err)
	}
	encabezados := amqp.Table{}
	for clave, valor := range guardado.Traza {
		encabezados[clave] = valor
	}

	publicador.mutex.Lock()
	defer publicador.mutex.Unlock()
	if err := publicador.conectar(); err != nil {
		return err
	}
	contexto, cancelar := context.WithTimeout(contexto, esperaPorPublicacion)
	defer cancelar()
	confirmacion, err := publicador.canal.PublishWithDeferredConfirmWithContext(contexto, Exchange, evento.Tipo, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    evento.ID,
		Timestamp:    evento.OcurridoEn,
		Headers:      encabezados,
		Body:         cuerpo,
	})
	if err != nil {
		publicador.desconectar()
		return fmt.Errorf("publicar el evento %s: %w", evento.ID, err)
	}
	confirmado, err := confirmacion.WaitContext(contexto)
	if err != nil {
		publicador.desconectar()
		return fmt.Errorf("esperar la confirmación del evento %s: %w", evento.ID, err)
	}
	if !confirmado {
		return fmt.Errorf("el broker no aceptó el evento %s", evento.ID)
	}
	return nil
}

// conectar abre la conexión y el canal si no están abiertos, y declara el exchange.
func (publicador *Publicador) conectar() error {
	if publicador.canal != nil && !publicador.canal.IsClosed() {
		return nil
	}
	publicador.desconectar()
	conexion, err := amqp.DialConfig(publicador.direccion, amqp.Config{Dial: amqp.DefaultDial(2 * time.Second)})
	if err != nil {
		return fmt.Errorf("conectar a RabbitMQ: %w", err)
	}
	canal, err := conexion.Channel()
	if err == nil {
		err = canal.Confirm(false)
	}
	if err == nil {
		err = canal.ExchangeDeclare(Exchange, amqp.ExchangeTopic, true, false, false, false, nil)
	}
	if err != nil {
		conexion.Close()
		return fmt.Errorf("preparar el canal de RabbitMQ: %w", err)
	}
	publicador.conexion, publicador.canal = conexion, canal
	return nil
}

func (publicador *Publicador) desconectar() {
	if publicador.conexion != nil {
		publicador.conexion.Close()
	}
	publicador.conexion, publicador.canal = nil, nil
}

// Cerrar cierra la conexión al apagar el servicio.
func (publicador *Publicador) Cerrar() {
	publicador.mutex.Lock()
	defer publicador.mutex.Unlock()
	publicador.desconectar()
}
