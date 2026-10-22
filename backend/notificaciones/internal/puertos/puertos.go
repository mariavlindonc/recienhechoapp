// Package puertos define, como interfaces, lo que la aplicación necesita del
// exterior: dónde guardar las notificaciones y cómo enviarlas. Los adaptadores
// (MongoDB, SMTP) las implementan; la aplicación no sabe cuál está usando.
package puertos

import (
	"context"
	"errors"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/dominio"
)

// ErrEnvioRechazado lo devuelve un Remitente cuando no tiene sentido reintentar,
// por ejemplo si el servidor de correo rechazó el destinatario. Cualquier otro
// error se considera transitorio y se reintenta.
var ErrEnvioRechazado = errors.New("el servidor de correo rechazó el envío")

// ClaveIdempotencia asocia la Idempotency-Key de un grupo con la notificación que
// creó y con la huella del contenido que se pidió.
type ClaveIdempotencia struct {
	Grupo          string
	Clave          string
	Huella         string
	IDNotificacion string
	CreadaEn       time.Time
}

// Repositorio guarda las notificaciones y las claves de idempotencia.
type Repositorio interface {
	// ReservarClave guarda la clave si no existía, en una sola operación atómica.
	// Si ya existía, no la modifica: devuelve la guardada y yaExistia en true.
	ReservarClave(contexto context.Context, clave ClaveIdempotencia) (guardada ClaveIdempotencia, yaExistia bool, err error)

	// CrearSiNoExiste guarda una notificación nueva. Si ya hay una con ese id, no
	// hace nada y no devuelve error.
	CrearSiNoExiste(contexto context.Context, notificacion dominio.Notificacion) error

	// Buscar devuelve la notificación del grupo con ese id, o dominio.ErrNoEncontrada.
	Buscar(contexto context.Context, grupo, id string) (dominio.Notificacion, error)

	// TomarPendiente elige una notificación pendiente lista para enviarse
	// (ProximoIntento <= ahora) y la aparta hasta el momento `hasta`, para que otra
	// instancia no la tome al mismo tiempo. Si no hay ninguna, encontrada es false.
	TomarPendiente(contexto context.Context, ahora, hasta time.Time) (notificacion dominio.Notificacion, encontrada bool, err error)

	// Actualizar guarda el nuevo estado de una notificación existente.
	Actualizar(contexto context.Context, notificacion dominio.Notificacion) error
}

// Mensaje es lo que se le entrega al Remitente.
type Mensaje struct {
	Destinatario string
	Asunto       string
	Cuerpo       string
}

// Remitente envía mensajes por algún canal (hoy, email por SMTP).
type Remitente interface {
	Enviar(contexto context.Context, mensaje Mensaje) error
}
