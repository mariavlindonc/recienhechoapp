// Package dominio tiene las reglas de negocio de los pedidos: qué se puede pedir,
// para qué fecha y por qué estados pasa un pedido (docs/SPEC.md). No conoce HTTP,
// MongoDB, RabbitMQ ni el inventario del proveedor; esos detalles viven en los
// adaptadores.
package dominio

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Estado de un pedido (docs/ARCHITECTURE.md, "Estados de un pedido").
type Estado string

const (
	EstadoPendienteConfirmacion Estado = "pendiente_confirmacion"
	EstadoConfirmado            Estado = "confirmado"
	EstadoEnProduccion          Estado = "en_produccion"
	EstadoListo                 Estado = "listo"
	// EstadoEnCamino es cuando el pedido salió a entregarse a domicilio. Es opcional:
	// lo que se retira en el local pasa de listo a entregado.
	EstadoEnCamino  Estado = "en_camino"
	EstadoEntregado Estado = "entregado"
	EstadoCancelado Estado = "cancelado"
)

// transiciones son los cambios de estado válidos (R6). No se puede cancelar en
// producción ni entregar algo que no está listo. Un envío pasa por en_camino; un
// retiro en el local va de listo a entregado.
var transiciones = map[Estado][]Estado{
	EstadoPendienteConfirmacion: {EstadoConfirmado, EstadoCancelado},
	EstadoConfirmado:            {EstadoEnProduccion, EstadoCancelado},
	EstadoEnProduccion:          {EstadoListo},
	EstadoListo:                 {EstadoEnCamino, EstadoEntregado},
	EstadoEnCamino:              {EstadoEntregado},
}

// Tipo de pedido: inmediato (el primer día con cupo, C4) o programado (fecha
// elegida, C5).
type Tipo string

const (
	TipoInmediato  Tipo = "inmediato"
	TipoProgramado Tipo = "programado"
)

const (
	cantidadMaxima    = 100
	itemsMaximos      = 20
	largoMaximoFranja = 50

	// formatoFecha es el de las fechas de producción: un día, sin hora.
	formatoFecha = "2006-01-02"

	esperaBaseConsumo   = time.Minute
	esperaMaximaConsumo = 10 * time.Minute
)

// zonaDeLaPasteleria define qué día es "hoy". Argentina no tiene horario de verano,
// así que alcanza con un desfase fijo y la imagen no necesita la base de zonas horarias.
var zonaDeLaPasteleria = time.FixedZone("ART", -3*60*60)

// Hoy es la fecha actual en la pastelería, con el formato de las fechas de producción.
func Hoy(ahora time.Time) string {
	return ahora.In(zonaDeLaPasteleria).Format(formatoFecha)
}

// SumarDias devuelve la fecha que está dias después de fecha. La fecha tiene que
// ser válida (ValidarFecha).
func SumarDias(fecha string, dias int) string {
	dia, _ := time.Parse(formatoFecha, fecha)
	return dia.AddDate(0, 0, dias).Format(formatoFecha)
}

// ValidarFecha controla que el texto sea una fecha AAAA-MM-DD.
func ValidarFecha(fecha string) error {
	if _, err := time.Parse(formatoFecha, fecha); err != nil {
		return fmt.Errorf("%w: la fecha %q no tiene el formato AAAA-MM-DD", ErrSolicitudInvalida, fecha)
	}
	return nil
}

// PrimeraFechaPosible es el primer día que se puede pedir un producto con esa
// anticipación mínima (R2).
func PrimeraFechaPosible(hoy string, anticipacionDias int) string {
	return SumarDias(hoy, anticipacionDias)
}

// ErrorDeAnticipacion indica que la fecha pedida no cumple la anticipación mínima
// y cuál es la primera fecha posible, para mostrársela al comprador.
type ErrorDeAnticipacion struct {
	PrimeraFecha string
}

func (problema ErrorDeAnticipacion) Error() string {
	return fmt.Sprintf("%v: la primera fecha posible es %s", ErrAnticipacionInsuficiente, problema.PrimeraFecha)
}

func (problema ErrorDeAnticipacion) Is(objetivo error) bool {
	return objetivo == ErrAnticipacionInsuficiente
}

// Comprador es quien hace el pedido. El email se copia al pedido para poder
// avisarle sin consultar a usuarios.
type Comprador struct {
	ID    string
	Email string
}

// ItemSolicitado es un producto y cuántas unidades se piden.
type ItemSolicitado struct {
	ProductoID string
	Cantidad   int
}

// Solicitud es lo que pide el comprador: uno o más productos para un mismo día (ADR-002).
type Solicitud struct {
	Tipo   Tipo
	Items  []ItemSolicitado
	Fecha  string // solo en los programados
	Franja string // opcional
}

// Validar controla la solicitud y devuelve ErrSolicitudInvalida con todos los
// problemas encontrados.
func (solicitud Solicitud) Validar() error {
	var problemas []string
	switch solicitud.Tipo {
	case TipoInmediato:
	case TipoProgramado:
		if err := ValidarFecha(solicitud.Fecha); err != nil {
			problemas = append(problemas, "fecha tiene que ser AAAA-MM-DD")
		}
	default:
		problemas = append(problemas, fmt.Sprintf("tipo %q inválido (inmediato o programado)", solicitud.Tipo))
	}
	if len(solicitud.Items) < 1 || len(solicitud.Items) > itemsMaximos {
		problemas = append(problemas, fmt.Sprintf("items tiene que tener entre 1 y %d productos", itemsMaximos))
	}
	vistos := map[string]bool{}
	for posicion, item := range solicitud.Items {
		if strings.TrimSpace(item.ProductoID) == "" {
			problemas = append(problemas, fmt.Sprintf("items[%d]: productoId es obligatorio", posicion))
		} else if vistos[item.ProductoID] {
			problemas = append(problemas, fmt.Sprintf("items[%d]: el producto %s está repetido", posicion, item.ProductoID))
		}
		vistos[item.ProductoID] = true
		if item.Cantidad < 1 || item.Cantidad > cantidadMaxima {
			problemas = append(problemas, fmt.Sprintf("items[%d]: cantidad tiene que estar entre 1 y %d", posicion, cantidadMaxima))
		}
	}
	if utf8.RuneCountInString(solicitud.Franja) > largoMaximoFranja {
		problemas = append(problemas, fmt.Sprintf("franja supera los %d caracteres", largoMaximoFranja))
	}
	if len(problemas) > 0 {
		return fmt.Errorf("%w: %s", ErrSolicitudInvalida, strings.Join(problemas, "; "))
	}
	return nil
}

// Huella identifica el contenido de la solicitud, para la idempotencia: la misma
// Idempotency-Key con otra huella es un conflicto. Los ítems se ordenan en una
// copia: el orden en que llegan no cambia qué se pidió.
func (solicitud Solicitud) Huella() string {
	partes := []string{string(solicitud.Tipo), solicitud.Fecha, solicitud.Franja}
	items := slices.Clone(solicitud.Items)
	slices.SortFunc(items, func(uno, otro ItemSolicitado) int { return strings.Compare(uno.ProductoID, otro.ProductoID) })
	for _, item := range items {
		partes = append(partes, item.ProductoID, strconv.Itoa(item.Cantidad))
	}
	contenido := strings.Join(partes, "\x00")
	suma := sha256.Sum256([]byte(contenido))
	return hex.EncodeToString(suma[:])
}

// Insumo es un ingrediente de la receta. En el pedido ya está multiplicado por las
// cantidades pedidas y sumado entre productos.
type Insumo struct {
	Ingrediente string
	Cantidad    int64
	Unidad      string
}

// Producto es lo que pedidos necesita saber del catálogo al crear un pedido.
type Producto struct {
	ID               string
	Nombre           string
	PrecioCentavos   int64
	AnticipacionDias int
	Receta           []Insumo // por unidad
}

// AnticipacionDe es la anticipación que necesita un pedido con esos productos:
// la del producto que más necesita (R2).
func AnticipacionDe(productos map[string]Producto) int {
	maxima := 0
	for _, producto := range productos {
		maxima = max(maxima, producto.AnticipacionDias)
	}
	return maxima
}

// Item es un producto del pedido con la copia de su nombre y su precio al momento
// de pedirlo (R4).
type Item struct {
	ProductoID             string
	Nombre                 string
	Cantidad               int
	PrecioUnitarioCentavos int64
	SubtotalCentavos       int64
}

// Pedido es un encargo de un comprador, con sus ítems y su estado.
type Pedido struct {
	ID                string
	Comprador         Comprador
	Items             []Item
	TotalCentavos     int64
	Insumos           []Insumo
	Fecha             string
	Franja            string
	Tipo              Tipo
	Estado            Estado
	MotivoCancelacion string
	// MensajeVendedor es lo que el vendedor le escribe al comprador al rechazar un
	// pedido especial sin cotizarlo; le llega en el mail.
	MensajeVendedor string
	// CotizadoEn es cuándo se cotizó un pedido especial; desde ahí corren las 48 h (R8).
	CotizadoEn time.Time

	ClaveIdempotencia string
	Huella            string

	RequiereRevision bool   // el vendedor tiene que decidir qué hacer
	MotivoRevision   string // por qué

	// InsumosReservados indica que el pedido tiene insumos reservados en el stock
	// propio y todavía no se consumieron todos (ADR-020). Cancelarlo los devuelve.
	InsumosReservados bool
	// Seguimiento del consumo en el proveedor, que empieza al pasar a producción
	// (ADR-020). El avance se guarda por insumo: si falla a la mitad, no se repite
	// lo que ya se descontó.
	ConsumoPendiente  bool
	ProximoConsumo    time.Time
	IntentosConsumo   int
	InsumosConsumidos int
	// ConsumoEnDuda es cuando la última llamada de reducir no tuvo respuesta: no se
	// sabe si se aplicó, y antes de repetirla hay que consultar el stock del proveedor.
	ConsumoEnDuda bool

	CreadoEn      time.Time
	ActualizadoEn time.Time
	// Version sube con cada cambio guardado: dos modificaciones simultáneas del
	// mismo pedido no se pisan (D4).
	Version int
}

// NuevoPedido arma un pedido pendiente de confirmación para la fecha dada.
// productos tiene los datos del catálogo de cada producto pedido. Controla la
// anticipación mínima (R2) y fija los precios (R4, R5).
func NuevoPedido(id string, comprador Comprador, clave string, solicitud Solicitud, productos map[string]Producto, fecha string, ahora time.Time) (Pedido, error) {
	if id == "" || comprador.ID == "" {
		return Pedido{}, fmt.Errorf("crear pedido: faltan el id o el comprador")
	}
	if err := solicitud.Validar(); err != nil {
		return Pedido{}, err
	}
	if err := ValidarFecha(fecha); err != nil {
		return Pedido{}, err
	}
	if primera := PrimeraFechaPosible(Hoy(ahora), AnticipacionDe(productos)); fecha < primera {
		return Pedido{}, ErrorDeAnticipacion{PrimeraFecha: primera}
	}

	pedido := Pedido{
		ID:                id,
		Comprador:         comprador,
		Fecha:             fecha,
		Franja:            solicitud.Franja,
		Tipo:              solicitud.Tipo,
		Estado:            EstadoPendienteConfirmacion,
		ClaveIdempotencia: clave,
		Huella:            solicitud.Huella(),
		CreadoEn:          ahora,
		ActualizadoEn:     ahora,
	}
	posicionDelInsumo := map[[2]string]int{} // (ingrediente, unidad) -> posición en Insumos
	for _, solicitado := range solicitud.Items {
		producto, existe := productos[solicitado.ProductoID]
		if !existe {
			return Pedido{}, fmt.Errorf("%w: %s", ErrProductoInexistente, solicitado.ProductoID)
		}
		subtotal := producto.PrecioCentavos * int64(solicitado.Cantidad)
		pedido.Items = append(pedido.Items, Item{
			ProductoID:             producto.ID,
			Nombre:                 producto.Nombre,
			Cantidad:               solicitado.Cantidad,
			PrecioUnitarioCentavos: producto.PrecioCentavos,
			SubtotalCentavos:       subtotal,
		})
		pedido.TotalCentavos += subtotal
		for _, insumo := range producto.Receta {
			cantidad := insumo.Cantidad * int64(solicitado.Cantidad)
			clave := [2]string{insumo.Ingrediente, insumo.Unidad}
			if posicion, existe := posicionDelInsumo[clave]; existe {
				pedido.Insumos[posicion].Cantidad += cantidad
				continue
			}
			posicionDelInsumo[clave] = len(pedido.Insumos)
			pedido.Insumos = append(pedido.Insumos, Insumo{Ingrediente: insumo.Ingrediente, Cantidad: cantidad, Unidad: insumo.Unidad})
		}
	}
	// La reserva de los insumos la guarda el repositorio junto con el pedido.
	pedido.InsumosReservados = len(pedido.Insumos) > 0
	return pedido, nil
}

// Unidades es cuántas unidades ocupa el pedido en el cupo del día: la suma de las
// cantidades de sus ítems.
func (pedido Pedido) Unidades() int {
	unidades := 0
	for _, item := range pedido.Items {
		unidades += item.Cantidad
	}
	return unidades
}

// PuedePasarA indica si el cambio de estado es válido (R6).
func (pedido Pedido) PuedePasarA(nuevo Estado) bool {
	for _, permitido := range transiciones[pedido.Estado] {
		if permitido == nuevo {
			return true
		}
	}
	return false
}

// CambiarEstado aplica una transición válida y devuelve el estado anterior.
// Cancelar devuelve la reserva de los insumos (R7) y pasar a producción deja el
// consumo pendiente (ADR-020). El cupo y el stock los actualiza el repositorio en
// la misma operación que guarda el cambio.
func (pedido *Pedido) CambiarEstado(nuevo Estado, motivo string, ahora time.Time) (Estado, error) {
	anterior := pedido.Estado
	if !pedido.PuedePasarA(nuevo) {
		return anterior, fmt.Errorf("%w: de %s a %s", ErrTransicionInvalida, anterior, nuevo)
	}
	pedido.Estado = nuevo
	pedido.ActualizadoEn = ahora
	pedido.RequiereRevision = false
	pedido.MotivoRevision = ""
	switch nuevo {
	case EstadoCancelado:
		pedido.MotivoCancelacion = motivo
		pedido.InsumosReservados = false
	case EstadoEnProduccion:
		pedido.ConsumoPendiente = pedido.InsumosReservados
		pedido.ProximoConsumo = ahora
	}
	return anterior, nil
}

// InsumoPorConsumir devuelve el próximo insumo que falta descontar en el proveedor,
// o false si ya no queda ninguno.
func (pedido Pedido) InsumoPorConsumir() (Insumo, bool) {
	if !pedido.ConsumoPendiente || pedido.InsumosConsumidos >= len(pedido.Insumos) {
		return Insumo{}, false
	}
	return pedido.Insumos[pedido.InsumosConsumidos], true
}

// InsumoConsumido registra que el proveedor descontó el insumo actual. Con el
// último, el consumo termina y no queda nada reservado.
func (pedido *Pedido) InsumoConsumido(ahora time.Time) {
	pedido.InsumosConsumidos++
	pedido.IntentosConsumo = 0
	pedido.ConsumoEnDuda = false
	pedido.ActualizadoEn = ahora
	if pedido.InsumosConsumidos >= len(pedido.Insumos) {
		pedido.ConsumoPendiente = false
		pedido.InsumosReservados = false
	}
}

// RegistrarFalloDeConsumo reprograma el consumo con esperas que se duplican (1, 2,
// 4, 8 minutos) hasta un máximo de 10. enDuda es si la llamada pudo haberse
// aplicado: antes de repetirla hay que consultar el stock del proveedor (ADR-020).
func (pedido *Pedido) RegistrarFalloDeConsumo(enDuda bool, ahora time.Time) {
	pedido.IntentosConsumo++
	pedido.ConsumoEnDuda = pedido.ConsumoEnDuda || enDuda
	pedido.ProximoConsumo = ahora.Add(EsperaAntesDeReintentar(pedido.IntentosConsumo))
	pedido.ActualizadoEn = ahora
}

// MarcarParaRevision deja el pedido para que lo resuelva el vendedor y frena el
// consumo: no se reintenta solo. No frena la producción. Suelta lo que quedaba
// reservado, porque ya nadie lo va a consumir (ADR-020).
func (pedido *Pedido) MarcarParaRevision(motivo string, ahora time.Time) {
	pedido.RequiereRevision = true
	pedido.MotivoRevision = motivo
	pedido.ConsumoPendiente = false
	pedido.InsumosReservados = false
	pedido.ActualizadoEn = ahora
}

// EsperaAntesDeReintentar devuelve cuánto esperar después de intentosHechos
// intentos fallidos: 1, 2, 4, 8 y después 10 minutos.
func EsperaAntesDeReintentar(intentosHechos int) time.Duration {
	if intentosHechos < 1 {
		return 0
	}
	if intentosHechos > 4 {
		return esperaMaximaConsumo
	}
	return min(esperaBaseConsumo<<(intentosHechos-1), esperaMaximaConsumo)
}

// PerteneceA indica si el pedido es del comprador dado.
func (pedido Pedido) PerteneceA(idComprador string) bool {
	return pedido.Comprador.ID == idComprador
}

// Dia es la capacidad de producción de una fecha y cuánto ya está reservado.
type Dia struct {
	Fecha      string
	Capacidad  int
	Reservados int
}
