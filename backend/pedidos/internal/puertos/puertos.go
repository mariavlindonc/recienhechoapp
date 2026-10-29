// Package puertos define, como interfaces, lo que la aplicación necesita del
// exterior: dónde guardar los pedidos, de dónde sacar los productos, cómo reservar
// insumos y cómo publicar eventos (ADR-007). Los adaptadores las implementan.
package puertos

import (
	"context"
	"errors"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
)

// Cambio es un pedido modificado que hay que guardar.
type Cambio struct {
	Pedido dominio.Pedido
	// VersionAnterior es la versión que se leyó: si en la base hay otra, alguien lo
	// modificó en el medio y el cambio no se guarda (dominio.ErrModificadoAlMismoTiempo).
	VersionAnterior int
	// Evento, si no es nil, se guarda en la bandeja de salida junto con el cambio.
	Evento *dominio.Evento
	// LiberarCupo devuelve al día el cupo del pedido (al cancelar, R7).
	LiberarCupo bool
}

// Repositorio guarda los pedidos, los cupos por día y la bandeja de salida de
// eventos (outbox). Cada método que escribe más de una cosa lo hace en una sola
// transacción: o se guarda todo o nada (D4).
type Repositorio interface {
	// Registrar guarda el pedido nuevo, reserva su cupo en el día y guarda el
	// evento. Devuelve dominio.ErrSinCupo si el día no alcanza, y
	// dominio.ErrPedidoRepetido si el comprador ya usó esa clave de idempotencia.
	Registrar(contexto context.Context, pedido dominio.Pedido, evento dominio.Evento) error

	// GuardarCambio guarda el pedido modificado, y en la misma transacción el
	// evento y la liberación del cupo, si corresponden.
	GuardarCambio(contexto context.Context, cambio Cambio) error

	// Buscar devuelve el pedido con ese id, o dominio.ErrNoEncontrado.
	Buscar(contexto context.Context, id string) (dominio.Pedido, error)

	// BuscarPorClave devuelve el pedido que el comprador creó con esa clave de
	// idempotencia, o dominio.ErrNoEncontrado.
	BuscarPorClave(contexto context.Context, idComprador, clave string) (dominio.Pedido, error)

	// ListarDeComprador devuelve los pedidos del comprador, del más nuevo al más viejo.
	ListarDeComprador(contexto context.Context, idComprador string) ([]dominio.Pedido, error)

	// ListarDelDia devuelve los pedidos que se producen en esa fecha (calendario, VE4).
	ListarDelDia(contexto context.Context, fecha string) ([]dominio.Pedido, error)

	// TomarParaConfirmar aparta hasta `hasta` un pedido pendiente de confirmación
	// cuya próxima confirmación ya llegó, para que otra instancia no lo tome.
	TomarParaConfirmar(contexto context.Context, ahora, hasta time.Time) (pedido dominio.Pedido, encontrado bool, err error)

	// TomarParaLiberar aparta un pedido con insumos por liberar en el proveedor.
	TomarParaLiberar(contexto context.Context, ahora, hasta time.Time) (pedido dominio.Pedido, encontrado bool, err error)

	// DefinirCapacidad fija la capacidad de un día. Devuelve
	// dominio.ErrCapacidadMenorQueReservados si ya hay más reservado.
	DefinirCapacidad(contexto context.Context, fecha string, capacidad int) (dominio.Dia, error)

	// ListarCapacidad devuelve los días con capacidad definida entre las dos fechas, inclusive.
	ListarCapacidad(contexto context.Context, desde, hasta string) ([]dominio.Dia, error)
}

// EventoGuardado es un evento de la bandeja de salida, con el contexto de la traza
// en la que se generó para continuarla al publicarlo.
type EventoGuardado struct {
	Evento dominio.Evento
	Traza  map[string]string
}

// BandejaDeSalida es la cola de eventos guardados que todavía no se publicaron.
type BandejaDeSalida interface {
	// TomarEvento aparta hasta `hasta` el evento más viejo sin publicar.
	TomarEvento(contexto context.Context, ahora, hasta time.Time) (evento EventoGuardado, encontrado bool, err error)
	// MarcarPublicado lo saca de la bandeja.
	MarcarPublicado(contexto context.Context, idEvento string) error
}

// Publicador manda un evento al broker. Devuelve nil solo cuando el broker
// confirmó que lo recibió.
type Publicador interface {
	Publicar(contexto context.Context, evento EventoGuardado) error
}

// ErrProductoNoDisponible lo devuelve el Catalogo cuando productos no respondió a
// tiempo: el pedido no se crea (ADR-003).
var ErrProductoNoDisponible = errors.New("el catálogo de productos no está disponible")

// Catalogo da los datos del producto al momento de crear el pedido.
type Catalogo interface {
	// ObtenerProducto devuelve dominio.ErrProductoInexistente si no existe o no se vende.
	ObtenerProducto(contexto context.Context, id string) (dominio.Producto, error)
}

// ResultadoReserva es la respuesta de negocio del proveedor de inventario.
type ResultadoReserva string

const (
	ReservaConfirmada ResultadoReserva = "confirmada"
	ReservaSinStock   ResultadoReserva = "sin_stock"
)

// ErrInventarioRechazo lo devuelve el ReservadorDeInsumos cuando el proveedor
// rechazó la solicitud por un error de formato (4xx inesperado): reintentar no lo
// arregla y lo tiene que revisar el equipo (ADR-010).
var ErrInventarioRechazo = errors.New("el inventario rechazó la solicitud")

// ErrInventarioNoDisponible lo devuelve el ReservadorDeInsumos cuando no intentó
// la llamada porque el disyuntor está abierto (ADR-011).
var ErrInventarioNoDisponible = errors.New("el inventario no está disponible")

// ReservadorDeInsumos es la capa anticorrupción frente al inventario del otro
// grupo (ADR-010). idPedido viaja como clave de idempotencia: repetir una reserva
// no reserva dos veces. Cualquier error que no sea ErrInventarioRechazo es
// transitorio y se reintenta.
type ReservadorDeInsumos interface {
	Reservar(contexto context.Context, idPedido string, insumos []dominio.Insumo) (ResultadoReserva, error)
	Liberar(contexto context.Context, idPedido string) error
}

// ResultadoInventario resume cómo terminó una llamada al inventario, para las métricas.
type ResultadoInventario string

const (
	InventarioConfirmada   ResultadoInventario = "confirmada"
	InventarioSinStock     ResultadoInventario = "sin_stock"
	InventarioLiberada     ResultadoInventario = "liberada"
	InventarioRechazada    ResultadoInventario = "rechazada"
	InventarioFallaTransit ResultadoInventario = "falla_transitoria"
	InventarioNoDisponible ResultadoInventario = "no_disponible"
)

// EstadoCircuito es el estado del disyuntor que protege al inventario.
type EstadoCircuito string

const (
	CircuitoCerrado     EstadoCircuito = "cerrado"
	CircuitoAbierto     EstadoCircuito = "abierto"
	CircuitoSemiabierto EstadoCircuito = "semiabierto"
)

// Eventos recibe los hechos que interesan fuera de la aplicación. La aplicación
// solo avisa qué pasó; el adaptador decide qué loguear y qué contar (ADR-006, ADR-007).
type Eventos interface {
	PedidoRegistrado(contexto context.Context, resultado string)
	InventarioConsultado(contexto context.Context, idPedido string, resultado ResultadoInventario, err error)
	CircuitoDelInventarioCambio(contexto context.Context, anterior, nuevo EstadoCircuito)
	EventoPublicado(contexto context.Context, idEvento string, err error)
}
