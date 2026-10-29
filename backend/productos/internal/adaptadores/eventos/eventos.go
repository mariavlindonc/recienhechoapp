// Package eventos implementa el puerto Eventos: recibe los hechos que avisa la
// aplicación y decide qué se loguea y qué se cuenta en las métricas (ADR-006,
// ADR-007). Así la aplicación no depende de slog ni de Prometheus.
package eventos

import (
	"context"
	"log/slog"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
)

// medidor es lo que este adaptador necesita de las métricas. Lo cumple *metricas.Metricas.
type medidor interface {
	ContarLecturaDeCache(tipo string, acierto bool)
	ContarErrorDeCache(operacion string)
	ContarSincronizacion(resultado string)
	MarcarAntiguedadDelIndice(segundos float64)
}

// Eventos cumple puertos.Eventos.
type Eventos struct {
	metricas medidor
	logger   *slog.Logger
}

var _ puertos.Eventos = (*Eventos)(nil)

// Nuevos arma el adaptador con las métricas y el logger del servicio.
func Nuevos(metricas medidor, logger *slog.Logger) *Eventos {
	return &Eventos{metricas: metricas, logger: logger}
}

func (eventos *Eventos) CacheConsultada(_ context.Context, tipo string, acierto bool) {
	eventos.metricas.ContarLecturaDeCache(tipo, acierto)
}

// CacheFallo va como WARN: el comprador solo nota la demora, pero si se repite
// la caché no está ayudando.
func (eventos *Eventos) CacheFallo(contexto context.Context, operacion string, err error) {
	eventos.metricas.ContarErrorDeCache(operacion)
	eventos.logger.WarnContext(contexto, "la caché no respondió; se siguió con MySQL", "operacion", operacion, "error", err.Error())
}

func (eventos *Eventos) IndiceSincronizado(contexto context.Context, cantidad int, err error) {
	if err != nil {
		eventos.metricas.ContarSincronizacion("error")
		eventos.logger.WarnContext(contexto, "no se pudo reflejar cambios en la búsqueda; se reintenta", "error", err.Error())
		return
	}
	eventos.metricas.ContarSincronizacion("ok")
	eventos.logger.InfoContext(contexto, "cambios reflejados en la búsqueda", "cantidad", cantidad)
}

func (eventos *Eventos) AntiguedadDelIndice(_ context.Context, antiguedad time.Duration) {
	eventos.metricas.MarcarAntiguedadDelIndice(antiguedad.Seconds())
}

func (eventos *Eventos) ImagenSinBorrar(contexto context.Context, url string, err error) {
	eventos.logger.WarnContext(contexto, "quedó un archivo de imagen sin usar", "url", url, "error", err.Error())
}
