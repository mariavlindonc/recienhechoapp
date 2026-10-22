// Package eventos implementa el puerto Eventos: recibe los hechos que avisa la
// aplicación y decide qué se loguea y qué se cuenta en las métricas (ADR-006,
// ADR-007). Así la aplicación no depende de slog ni de Prometheus.
package eventos

import (
	"context"
	"log/slog"
	"regexp"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

// contadorDeEnvios es lo que este adaptador necesita de las métricas. Lo cumple
// *metricas.Metricas; en los tests se usa uno falso.
type contadorDeEnvios interface {
	ContarEnvio(resultado puertos.ResultadoEnvio)
}

// Eventos cumple puertos.Eventos.
type Eventos struct {
	metricas contadorDeEnvios
	logger   *slog.Logger
}

var _ puertos.Eventos = (*Eventos)(nil)

// Nuevos arma el adaptador con las métricas y el logger del servicio.
func Nuevos(metricas contadorDeEnvios, logger *slog.Logger) *Eventos {
	return &Eventos{metricas: metricas, logger: logger}
}

// EnvioIntentado cuenta cada intento por resultado y deja en el log los que
// fallaron, sin destinatario ni contenido del mail.
func (eventos *Eventos) EnvioIntentado(contexto context.Context, intento puertos.IntentoDeEnvio) {
	eventos.metricas.ContarEnvio(intento.Resultado)
	if intento.Error == nil {
		return
	}
	eventos.logger.WarnContext(contexto, "envío de notificación fallido",
		"id", intento.IDNotificacion,
		"intento", intento.Numero,
		"resultado", intento.Resultado,
		"error", ocultarEmails(intento.Error.Error()),
	)
}

// patronEmail reconoce cualquier cosa con forma de dirección de email.
var patronEmail = regexp.MustCompile(`[^\s<>@"']+@[^\s<>@"']+`)

// ocultarEmails tapa las direcciones de email de un texto antes de registrarlo:
// algunos servidores de correo incluyen al destinatario en su respuesta
// (por ejemplo, "550 <cliente@ejemplo.com> no existe").
func ocultarEmails(texto string) string {
	return patronEmail.ReplaceAllString(texto, "[email oculto]")
}
