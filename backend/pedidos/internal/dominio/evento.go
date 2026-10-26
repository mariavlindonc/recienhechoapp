package dominio

import "time"

// TipoEstadoCambiado es el evento que se publica cada vez que un pedido se crea o
// cambia de estado (ADR-003). notificaciones lo consume para avisarle al comprador.
const TipoEstadoCambiado = "pedido.estado_cambiado"

// versionEvento es la versión del formato del evento; sube si cambia de forma incompatible.
const versionEvento = 1

// Evento es un hecho del dominio que se publica por mensajería.
type Evento struct {
	ID         string // idEvento: el consumidor descarta los repetidos
	Tipo       string
	Version    int
	OcurridoEn time.Time
	Datos      DatosEstadoCambiado
}

// DatosEstadoCambiado lleva lo necesario para avisarle al comprador sin que
// notificaciones tenga que consultar a otro servicio.
type DatosEstadoCambiado struct {
	IDPedido       string
	IDComprador    string
	EmailComprador string
	EstadoAnterior string // vacío cuando el pedido se acaba de crear
	EstadoNuevo    string
	NombreProducto string
	Cantidad       int
	Fecha          string
	Motivo         string // de la cancelación, si hay
}

// NuevoEventoEstadoCambiado describe el estado actual del pedido y de cuál venía.
func NuevoEventoEstadoCambiado(id string, pedido Pedido, anterior Estado, ahora time.Time) Evento {
	return Evento{
		ID:         id,
		Tipo:       TipoEstadoCambiado,
		Version:    versionEvento,
		OcurridoEn: ahora,
		Datos: DatosEstadoCambiado{
			IDPedido:       pedido.ID,
			IDComprador:    pedido.Comprador.ID,
			EmailComprador: pedido.Comprador.Email,
			EstadoAnterior: string(anterior),
			EstadoNuevo:    string(pedido.Estado),
			NombreProducto: pedido.NombreProducto,
			Cantidad:       pedido.Cantidad,
			Fecha:          pedido.Fecha,
			Motivo:         pedido.MotivoCancelacion,
		},
	}
}
