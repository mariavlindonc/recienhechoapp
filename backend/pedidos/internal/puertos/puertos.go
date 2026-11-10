// Package puertos define, como interfaces, lo que la aplicación necesita del
// exterior: dónde guardar los pedidos y el stock propio, de dónde sacar los
// productos, cómo consumir el stock del proveedor y cómo publicar eventos
// (ADR-007, ADR-020). Los adaptadores las implementan.
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
	// ReservarCupo ocupa el cupo del día (al aceptar un especial). Si no alcanza,
	// no se guarda nada y devuelve dominio.ErrSinCupo.
	ReservarCupo bool
	// LiberarReserva devuelve al stock propio los insumos que el pedido tenía
	// reservados (al cancelar, R7). No llama al proveedor (ADR-020).
	LiberarReserva bool
	// Consumo, si no es nil, pasa a stock lo que el proveedor descontó de un insumo.
	Consumo *Consumo
}

// Consumo es un insumo que el proveedor descontó para un pedido: el stock propio
// queda en lo que el proveedor devolvió y la reserva baja en la cantidad consumida.
type Consumo struct {
	Insumo dominio.Insumo
	// StockQueda es el stock que informó el proveedor después de descontar.
	StockQueda int64
}

// Repositorio guarda los pedidos, los cupos por día y la bandeja de salida de
// eventos (outbox). Cada método que escribe más de una cosa lo hace en una sola
// transacción: o se guarda todo o nada (D4).
type Repositorio interface {
	// Registrar guarda el pedido nuevo, reserva su cupo en el día y sus insumos en
	// el stock propio, y guarda el evento. Los insumos se reservan aunque no
	// alcancen: la reserva nunca rechaza un pedido (ADR-020). Devuelve
	// dominio.ErrSinCupo si el día no alcanza, y dominio.ErrPedidoRepetido si el
	// comprador ya usó esa clave de idempotencia.
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

	// ListarEspecialesAbiertos devuelve los especiales solicitados o cotizados (VE5).
	ListarEspecialesAbiertos(contexto context.Context) ([]dominio.Pedido, error)

	// ListarPendientesDeConfirmar devuelve los pedidos que esperan al vendedor, del
	// más viejo al más nuevo.
	ListarPendientesDeConfirmar(contexto context.Context) ([]dominio.Pedido, error)

	// TomarParaConsumir aparta hasta `hasta` un pedido en producción con insumos por
	// consumir cuyo próximo intento ya llegó, para que nadie más lo procese mientras
	// tanto. Va de a uno por vez (ADR-020).
	TomarParaConsumir(contexto context.Context, ahora, hasta time.Time) (pedido dominio.Pedido, encontrado bool, err error)

	// ContarConsumosPendientes devuelve cuántos pedidos esperan que el proveedor
	// descuente sus insumos.
	ContarConsumosPendientes(contexto context.Context) (int, error)

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
	// DevolverEvento le quita el apartado a un evento que no se pudo publicar, para
	// que sea el próximo en tomarse y no se le adelanten los más nuevos.
	DevolverEvento(contexto context.Context, idEvento string, ahora time.Time) error
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

// Existencias guarda el stock propio y las reservas de los pedidos, en la base
// `inventario` (ADR-020). Las reservas y los consumos que van junto con un pedido
// se guardan por el Repositorio, en la misma transacción.
type Existencias interface {
	// Listar devuelve el stock de todos los ingredientes que se conocen.
	Listar(contexto context.Context) ([]dominio.Existencia, error)

	// Obtener devuelve la existencia del ingrediente, y la crea en cero si todavía
	// no existe.
	Obtener(contexto context.Context, ingrediente, unidad string) (dominio.Existencia, error)

	// AsignarProducto le da al ingrediente un producto libre del proveedor, si no
	// tenía. Devuelve ErrSinProductoDelProveedor si ya están todos usados.
	AsignarProducto(contexto context.Context, ingrediente, unidad string) (dominio.Existencia, error)

	// GuardarStock deja como stock conocido el que informó el proveedor.
	GuardarStock(contexto context.Context, ingrediente, unidad string, stock int64) error
}

// ErrSinProductoDelProveedor lo devuelve Existencias cuando el proveedor no tiene
// más productos para usar de casillero: el ingrediente queda sin producto y el
// pedido se marca para el vendedor (ADR-020).
var ErrSinProductoDelProveedor = errors.New("no quedan productos del proveedor para este ingrediente")

// ErrInventarioRechazo lo devuelve el ProveedorDeStock cuando el proveedor
// rechazó la solicitud por un error de formato (4xx inesperado): reintentar no lo
// arregla y lo tiene que revisar el equipo (ADR-020).
var ErrInventarioRechazo = errors.New("el inventario rechazó la solicitud")

// ErrInventarioNoDisponible lo devuelve el ProveedorDeStock cuando no intentó la
// llamada porque el disyuntor está abierto (ADR-011).
var ErrInventarioNoDisponible = errors.New("el inventario no está disponible")

// ErrStockInsuficiente lo devuelve el ProveedorDeStock cuando reducir no alcanza
// (409 del proveedor): el stock no cambió y no hay que reintentar (ADR-020).
var ErrStockInsuficiente = errors.New("el stock del proveedor no alcanza")

// ProveedorDeStock es la capa anticorrupción frente al stock del otro grupo
// (ADR-020). Reducir y Aumentar devuelven el stock que queda. No son idempotentes:
// si no llegó la respuesta, hay que Consultar antes de repetir. Cualquier error que
// no sea ErrStockInsuficiente ni ErrInventarioRechazo es transitorio.
type ProveedorDeStock interface {
	Reducir(contexto context.Context, idProducto, cantidad int64, motivo string) (stock int64, err error)
	Aumentar(contexto context.Context, idProducto, cantidad int64, motivo string) (stock int64, err error)
	Consultar(contexto context.Context, idProducto int64) (stock int64, err error)
}

// ResultadoInventario resume cómo terminó una llamada al inventario, para las métricas.
type ResultadoInventario string

const (
	InventarioConsumida        ResultadoInventario = "consumida"
	InventarioCargada          ResultadoInventario = "cargada"
	InventarioSinStock         ResultadoInventario = "sin_stock"
	InventarioRechazada        ResultadoInventario = "rechazada"
	InventarioFallaTransitoria ResultadoInventario = "falla_transitoria"
	InventarioNoDisponible     ResultadoInventario = "no_disponible"
)

// EstadoCircuito es el estado del disyuntor que protege al inventario.
type EstadoCircuito string

const (
	CircuitoCerrado     EstadoCircuito = "cerrado"
	CircuitoAbierto     EstadoCircuito = "abierto"
	CircuitoSemiabierto EstadoCircuito = "semiabierto"
)

// AvisosEnVivo les avisa a los compradores conectados que cambió un pedido suyo
// (ADR-018). La aplicación lo llama solo después de guardar, así lo que se avisa ya
// está en la base. No puede frenar a quien llama: si un comprador no recibe a
// tiempo, el aviso no se reintenta, porque la pantalla pide el estado al reconectarse.
type AvisosEnVivo interface {
	Avisar(evento dominio.Evento)
}

// Eventos recibe los hechos que interesan fuera de la aplicación. La aplicación
// solo avisa qué pasó; el adaptador decide qué loguear y qué contar (ADR-006, ADR-007).
type Eventos interface {
	PedidoRegistrado(contexto context.Context, resultado string)
	InventarioConsultado(contexto context.Context, idPedido string, resultado ResultadoInventario, err error)
	CircuitoDelInventarioCambio(contexto context.Context, anterior, nuevo EstadoCircuito)
	EventoPublicado(contexto context.Context, idEvento string, err error)
	// ConsumosPendientes avisa cuántos pedidos esperan que el proveedor descuente sus
	// insumos, en cada vuelta del proceso en segundo plano.
	ConsumosPendientes(contexto context.Context, cantidad int)
}
