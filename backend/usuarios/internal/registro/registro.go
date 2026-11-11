// Package registro arma los logs del servicio: JSON con el nombre del servicio y,
// cuando la solicitud tiene una traza, su trace_id y span_id (ADR-006). Así los
// logs de distintos servicios que atendieron la misma solicitud se pueden juntar.
package registro

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

// NuevoLogger devuelve un logger que escribe una línea JSON por evento.
func NuevoLogger(salida io.Writer, servicio string) *slog.Logger {
	return slog.New(manejadorConTraza{slog.NewJSONHandler(salida, nil)}).With("servicio", servicio)
}

// manejadorConTraza envuelve a otro manejador de slog y le agrega a cada línea el
// trace_id y el span_id del contexto. Sin traza activa, escribe la línea igual.
type manejadorConTraza struct {
	slog.Handler
}

func (manejador manejadorConTraza) Handle(contexto context.Context, linea slog.Record) error {
	if traza := trace.SpanContextFromContext(contexto); traza.IsValid() {
		linea.AddAttrs(
			slog.String("trace_id", traza.TraceID().String()),
			slog.String("span_id", traza.SpanID().String()),
		)
	}
	return manejador.Handler.Handle(contexto, linea)
}

func (manejador manejadorConTraza) WithAttrs(atributos []slog.Attr) slog.Handler {
	return manejadorConTraza{manejador.Handler.WithAttrs(atributos)}
}

func (manejador manejadorConTraza) WithGroup(nombre string) slog.Handler {
	return manejadorConTraza{manejador.Handler.WithGroup(nombre)}
}

// Solicitudes es un middleware de Gin que deja una línea por cada solicitud
// atendida. No registra headers ni cuerpos: los de usuarios traen contraseñas.
func Solicitudes(logger *slog.Logger) gin.HandlerFunc {
	return func(contexto *gin.Context) {
		inicio := time.Now()
		contexto.Next()

		ruta := contexto.FullPath()
		if ruta == "" {
			ruta = "sin_ruta"
		}
		logger.InfoContext(contexto.Request.Context(), "solicitud atendida",
			"metodo", contexto.Request.Method,
			"ruta", ruta,
			"estado", contexto.Writer.Status(),
			"duracion_ms", time.Since(inicio).Milliseconds(),
		)
	}
}
