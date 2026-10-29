// Package eventos implementa el puerto Eventos: recibe los hechos que avisa la
// aplicación y decide qué se loguea y qué se cuenta en las métricas (ADR-006,
// ADR-007). Así la aplicación no depende de slog ni de Prometheus.
package eventos

import (
	"context"
	"log/slog"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

// medidor es lo que este adaptador necesita de las métricas. Lo cumple
// *metricas.Metricas; en los tests se usa uno falso.
type medidor interface {
	ContarRegistro(resultado string)
	ContarInventario(resultado puertos.ResultadoInventario)
	MarcarCircuito(estado puertos.EstadoCircuito)
	ContarPublicacion(resultado string)
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

// PedidoRegistrado cuenta los pedidos aceptados y los rechazados por falta de cupo.
func (eventos *Eventos) PedidoRegistrado(contexto context.Context, resultado string) {
	eventos.metricas.ContarRegistro(resultado)
}

// InventarioConsultado cuenta cada llamada y deja en el log las que fallaron. Un
// rechazo por formato va como ERROR: es un error nuestro o un cambio del contrato
// del proveedor, y lo tiene que revisar el equipo (ADR-010).
func (eventos *Eventos) InventarioConsultado(contexto context.Context, idPedido string, resultado puertos.ResultadoInventario, err error) {
	eventos.metricas.ContarInventario(resultado)
	if err == nil {
		return
	}
	nivel := slog.LevelWarn
	if resultado == puertos.InventarioRechazada {
		nivel = slog.LevelError
	}
	eventos.logger.Log(contexto, nivel, "llamada al inventario fallida", "id_pedido", idPedido, "resultado", resultado, "error", err.Error())
}

// CircuitoDelInventarioCambio actualiza la métrica del disyuntor y lo deja en el
// log: como WARN cuando se abre y como INFO en los demás casos.
func (eventos *Eventos) CircuitoDelInventarioCambio(contexto context.Context, anterior, nuevo puertos.EstadoCircuito) {
	eventos.metricas.MarcarCircuito(nuevo)
	nivel := slog.LevelInfo
	if nuevo == puertos.CircuitoAbierto {
		nivel = slog.LevelWarn
	}
	eventos.logger.Log(contexto, nivel, "cambió el estado del disyuntor del inventario", "anterior", anterior, "nuevo", nuevo)
}

// EventoPublicado cuenta las publicaciones y deja en el log las que fallaron.
func (eventos *Eventos) EventoPublicado(contexto context.Context, idEvento string, err error) {
	if err == nil {
		eventos.metricas.ContarPublicacion("publicado")
		return
	}
	eventos.metricas.ContarPublicacion("error")
	eventos.logger.WarnContext(contexto, "no se pudo publicar un evento; queda en la bandeja de salida", "id_evento", idEvento, "error", err.Error())
}
