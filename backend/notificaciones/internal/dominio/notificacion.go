// Package dominio tiene las reglas de negocio de las notificaciones: qué datos son
// válidos y por qué estados pasa un envío. No conoce HTTP, MongoDB ni SMTP; esos
// detalles viven en los adaptadores.
package dominio

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

// Estado de una notificación, con los mismos valores que el contrato.
type Estado string

const (
	EstadoPendiente Estado = "pendiente"
	EstadoEnviada   Estado = "enviada"
	EstadoFallida   Estado = "fallida"
)

// Canal de envío. Por ahora el contrato solo admite email.
type Canal string

const CanalEmail Canal = "email"

// Límites del contrato (NuevaNotificacion en notificaciones.yaml).
const (
	largoMaximoDestinatario = 254
	largoMaximoAsunto       = 150
	largoMaximoCuerpo       = 5000
)

// ReintentosMaximos es cuántas veces se reintenta un envío que falló por un error
// transitorio (servidor de correo caído, timeout) antes de darlo por fallido.
const ReintentosMaximos = 3

// esperaBaseReintento es la espera antes del primer reintento; se duplica en cada uno.
const esperaBaseReintento = 30 * time.Second

// VidaMaximaPendiente es cuánto puede quedar pendiente una notificación. Con los
// reintentos normales nunca se llega; pasa cuando el servidor de correo estuvo
// caído y la notificación se fue postergando (ADR-011).
const VidaMaximaPendiente = 24 * time.Hour

// MotivoVencida es el motivo de una notificación que venció sin poder enviarse.
const MotivoVencida = "El servidor de correo no estuvo disponible durante 24 horas."

// Solicitud es lo que pide el consumidor en el POST /notificaciones.
type Solicitud struct {
	Canal        Canal
	Destinatario string
	Asunto       string
	Cuerpo       string
}

// Validar controla la solicitud contra el contrato y devuelve ErrSolicitudInvalida
// con todos los problemas encontrados, no solo el primero.
func (solicitud Solicitud) Validar() error {
	var problemas []string

	if solicitud.Canal != CanalEmail {
		problemas = append(problemas, fmt.Sprintf("canal %q no soportado (solo %q)", solicitud.Canal, CanalEmail))
	}
	if !esEmailValido(solicitud.Destinatario) {
		problemas = append(problemas, "destinatario no es un email válido")
	}
	if problema := controlarLargo("asunto", solicitud.Asunto, largoMaximoAsunto); problema != "" {
		problemas = append(problemas, problema)
	}
	if problema := controlarLargo("cuerpo", solicitud.Cuerpo, largoMaximoCuerpo); problema != "" {
		problemas = append(problemas, problema)
	}

	if len(problemas) > 0 {
		return fmt.Errorf("%w: %s", ErrSolicitudInvalida, strings.Join(problemas, "; "))
	}
	return nil
}

// Huella identifica el contenido de la solicitud. Sirve para la idempotencia: si
// llega la misma Idempotency-Key con la misma huella es un reintento; con otra
// huella es un conflicto.
func (solicitud Solicitud) Huella() string {
	// El separador \x00 evita que ("ab", "c") y ("a", "bc") den la misma huella.
	contenido := strings.Join([]string{
		string(solicitud.Canal), solicitud.Destinatario, solicitud.Asunto, solicitud.Cuerpo,
	}, "\x00")
	suma := sha256.Sum256([]byte(contenido))
	return hex.EncodeToString(suma[:])
}

// Notificacion es un envío pedido por un grupo, con su estado.
type Notificacion struct {
	ID           string
	Grupo        string // dueño: el grupo al que pertenece la clave de API que la creó
	Canal        Canal
	Destinatario string
	Asunto       string
	Cuerpo       string
	Estado       Estado
	Motivo       string // por qué falló; solo cuando Estado es fallida
	Intentos     int    // intentos de envío hechos
	// ProximoIntento es desde cuándo se puede volver a intentar el envío.
	ProximoIntento time.Time
	CreadaEn       time.Time
	EnviadaEn      time.Time // cero mientras no se haya enviado
}

// NuevaNotificacion crea una notificación pendiente, lista para enviarse ya.
func NuevaNotificacion(id, grupo string, solicitud Solicitud, ahora time.Time) (Notificacion, error) {
	if id == "" || grupo == "" {
		return Notificacion{}, fmt.Errorf("crear notificación: faltan el id o el grupo")
	}
	if err := solicitud.Validar(); err != nil {
		return Notificacion{}, err
	}
	return Notificacion{
		ID:             id,
		Grupo:          grupo,
		Canal:          solicitud.Canal,
		Destinatario:   solicitud.Destinatario,
		Asunto:         solicitud.Asunto,
		Cuerpo:         solicitud.Cuerpo,
		Estado:         EstadoPendiente,
		ProximoIntento: ahora,
		CreadaEn:       ahora,
	}, nil
}

// PerteneceA indica si la notificación es del grupo dado. Un grupo nunca ve las
// notificaciones de otro.
func (notificacion Notificacion) PerteneceA(grupo string) bool {
	return notificacion.Grupo == grupo
}

// MarcarEnviada registra que el servidor de correo aceptó el envío.
func (notificacion *Notificacion) MarcarEnviada(ahora time.Time) error {
	if notificacion.Estado != EstadoPendiente {
		return fmt.Errorf("%w: de %s a %s", ErrTransicionInvalida, notificacion.Estado, EstadoEnviada)
	}
	notificacion.Intentos++
	notificacion.Estado = EstadoEnviada
	notificacion.EnviadaEn = ahora
	return nil
}

// RegistrarFalloTransitorio registra un envío que falló por algo que se puede
// arreglar solo (servidor caído, timeout). Si quedan reintentos, la notificación
// sigue pendiente y se reprograma con espera exponencial; si no, pasa a fallida.
func (notificacion *Notificacion) RegistrarFalloTransitorio(motivo string, ahora time.Time) error {
	if notificacion.Estado != EstadoPendiente {
		return fmt.Errorf("%w: de %s a reintento", ErrTransicionInvalida, notificacion.Estado)
	}
	notificacion.Intentos++
	if notificacion.Intentos > ReintentosMaximos {
		notificacion.Estado = EstadoFallida
		notificacion.Motivo = "se agotaron los reintentos: " + motivo
		return nil
	}
	notificacion.ProximoIntento = ahora.Add(EsperaAntesDelReintento(notificacion.Intentos))
	return nil
}

// RegistrarRechazo registra un envío que no tiene sentido reintentar (por ejemplo,
// el servidor de correo rechazó el destinatario): pasa directo a fallida.
func (notificacion *Notificacion) RegistrarRechazo(motivo string) error {
	if notificacion.Estado != EstadoPendiente {
		return fmt.Errorf("%w: de %s a %s", ErrTransicionInvalida, notificacion.Estado, EstadoFallida)
	}
	notificacion.Intentos++
	notificacion.Estado = EstadoFallida
	notificacion.Motivo = motivo
	return nil
}

// Postergar corre el próximo intento sin contarlo como intento: se usa cuando el
// envío ni se intentó porque el servidor de correo no está disponible.
func (notificacion *Notificacion) Postergar(hasta time.Time) error {
	if notificacion.Estado != EstadoPendiente {
		return fmt.Errorf("%w: no se puede postergar una notificación %s", ErrTransicionInvalida, notificacion.Estado)
	}
	notificacion.ProximoIntento = hasta
	return nil
}

// Vencida indica si la notificación lleva pendiente más de VidaMaximaPendiente.
func (notificacion Notificacion) Vencida(ahora time.Time) bool {
	return notificacion.Estado == EstadoPendiente && ahora.Sub(notificacion.CreadaEn) >= VidaMaximaPendiente
}

// MarcarVencida pasa a fallida una notificación que no se pudo enviar a tiempo.
func (notificacion *Notificacion) MarcarVencida() error {
	if notificacion.Estado != EstadoPendiente {
		return fmt.Errorf("%w: de %s a %s", ErrTransicionInvalida, notificacion.Estado, EstadoFallida)
	}
	notificacion.Estado = EstadoFallida
	notificacion.Motivo = MotivoVencida
	return nil
}

// EsperaAntesDelReintento devuelve cuánto esperar después de intentosHechos
// envíos fallidos: 30 s, 1 min, 2 min...
func EsperaAntesDelReintento(intentosHechos int) time.Duration {
	if intentosHechos < 1 {
		return 0
	}
	return esperaBaseReintento << (intentosHechos - 1)
}

// esEmailValido acepta solo una dirección sola ("cliente@ejemplo.com"), sin nombre
// ("Cliente <cliente@ejemplo.com>") ni espacios alrededor.
func esEmailValido(destinatario string) bool {
	if destinatario == "" || utf8.RuneCountInString(destinatario) > largoMaximoDestinatario {
		return false
	}
	direccion, err := mail.ParseAddress(destinatario)
	if err != nil {
		return false
	}
	return direccion.Name == "" && direccion.Address == destinatario
}

// controlarLargo cuenta caracteres (no bytes), como JSON Schema: "ñ" cuenta uno.
func controlarLargo(campo, valor string, maximo int) string {
	largo := utf8.RuneCountInString(valor)
	switch {
	case largo == 0:
		return campo + " es obligatorio"
	case largo > maximo:
		return fmt.Sprintf("%s supera los %d caracteres", campo, maximo)
	}
	return ""
}
