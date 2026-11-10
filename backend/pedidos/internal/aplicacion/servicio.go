// Package aplicacion tiene los casos de uso de pedidos: encargar (con
// idempotencia), cancelar, avanzar estados, definir la capacidad de cada día,
// cargar el stock y los procesos en segundo plano (consumir los insumos en el
// proveedor y publicar eventos). Usa el dominio para las reglas y los puertos para
// todo lo demás.
package aplicacion

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

const (
	largoMaximoClave = 100

	// diasDeBusqueda es hasta cuántos días después de la primera fecha posible se
	// busca cupo para un pedido inmediato.
	diasDeBusqueda = 30

	// tiempoApartado es cuánto queda reservado un pedido para quien lo tomó (una
	// solicitud o el proceso en segundo plano), así no lo procesan los dos a la vez.
	// Si el servicio se cae a mitad de camino, pasado este tiempo se vuelve a tomar.
	tiempoApartado = 2 * time.Minute

	// tiempoApartadoEvento es lo mismo para un evento de la bandeja de salida. Es
	// más corto porque publicar corta a los 5 s: si el broker estaba caído, el
	// evento se vuelve a intentar enseguida que vuelve.
	tiempoApartadoEvento = 15 * time.Second

	// largoMaximoIngrediente y largoMaximoUnidad acotan lo que escribe el vendedor al
	// cargar stock.
	largoMaximoIngrediente = 100
	largoMaximoUnidad      = 20
	cantidadMaximaCarga    = 1_000_000
)

// Servicio reúne los casos de uso.
type Servicio struct {
	repositorio puertos.Repositorio
	bandeja     puertos.BandejaDeSalida
	publicador  puertos.Publicador
	catalogo    puertos.Catalogo
	existencias puertos.Existencias
	inventario  puertos.ProveedorDeStock
	eventos     puertos.Eventos
	avisos      puertos.AvisosEnVivo
	ahora       func() time.Time
	nuevoID     func() string
}

// Dependencias del servicio. El reloj y el generador de ids se reciben de afuera
// para que los tests puedan controlarlos.
type Dependencias struct {
	Repositorio puertos.Repositorio
	Bandeja     puertos.BandejaDeSalida
	Publicador  puertos.Publicador
	Catalogo    puertos.Catalogo
	Existencias puertos.Existencias
	Inventario  puertos.ProveedorDeStock
	Eventos     puertos.Eventos
	Avisos      puertos.AvisosEnVivo
	Ahora       func() time.Time
	NuevoID     func() string
}

// NuevoServicio arma el servicio.
func NuevoServicio(dependencias Dependencias) *Servicio {
	return &Servicio{
		repositorio: dependencias.Repositorio,
		bandeja:     dependencias.Bandeja,
		publicador:  dependencias.Publicador,
		catalogo:    dependencias.Catalogo,
		existencias: dependencias.Existencias,
		inventario:  dependencias.Inventario,
		eventos:     dependencias.Eventos,
		avisos:      dependencias.Avisos,
		ahora:       dependencias.Ahora,
		nuevoID:     dependencias.NuevoID,
	}
}

// Crear encarga un pedido: fija el precio, reserva el cupo del día y los insumos
// de la receta, y lo deja pendiente de confirmación hasta que el vendedor lo
// revise (ADR-020). No llama al proveedor. Repetir el pedido con la misma clave
// devuelve el ya creado, sin reservar nada otra vez (R1).
func (servicio *Servicio) Crear(contexto context.Context, comprador dominio.Comprador, clave string, solicitud dominio.Solicitud) (dominio.Pedido, error) {
	if err := validarClave(clave); err != nil {
		return dominio.Pedido{}, err
	}
	if err := solicitud.Validar(); err != nil {
		return dominio.Pedido{}, err
	}
	if existente, encontrado, err := servicio.pedidoYaCreado(contexto, comprador.ID, clave, solicitud); err != nil || encontrado {
		return existente, err
	}

	// shortcut: los productos se piden de a uno, así que la demora crece con la
	// cantidad de ítems; se piden en paralelo si los tests de carga lo muestran lento.
	productos := make(map[string]dominio.Producto, len(solicitud.Items))
	for _, item := range solicitud.Items {
		producto, err := servicio.catalogo.ObtenerProducto(contexto, item.ProductoID)
		if err != nil {
			return dominio.Pedido{}, fmt.Errorf("obtener producto %s: %w", item.ProductoID, err)
		}
		productos[item.ProductoID] = producto
	}

	pedido, err := servicio.registrar(contexto, comprador, clave, solicitud, productos)
	if errors.Is(err, dominio.ErrPedidoRepetido) {
		// Otro intento con la misma clave ganó la carrera: se devuelve ese.
		existente, _, errExistente := servicio.pedidoYaCreado(contexto, comprador.ID, clave, solicitud)
		return existente, errExistente
	}
	return pedido, err
}

// pedidoYaCreado busca el pedido de un intento anterior con la misma clave.
func (servicio *Servicio) pedidoYaCreado(contexto context.Context, idComprador, clave string, solicitud dominio.Solicitud) (dominio.Pedido, bool, error) {
	existente, err := servicio.repositorio.BuscarPorClave(contexto, idComprador, clave)
	if errors.Is(err, dominio.ErrNoEncontrado) {
		return dominio.Pedido{}, false, nil
	}
	if err != nil {
		return dominio.Pedido{}, false, fmt.Errorf("buscar pedido por clave de idempotencia: %w", err)
	}
	if existente.Huella != solicitud.Huella() {
		return dominio.Pedido{}, false, dominio.ErrConflictoIdempotencia
	}
	return existente, true, nil
}

// registrar guarda el pedido con su cupo. Un programado va a su fecha; un
// inmediato prueba desde la primera fecha posible hasta encontrar un día con cupo.
func (servicio *Servicio) registrar(contexto context.Context, comprador dominio.Comprador, clave string, solicitud dominio.Solicitud, productos map[string]dominio.Producto) (dominio.Pedido, error) {
	ahora := servicio.ahora()
	fechas := []string{solicitud.Fecha}
	if solicitud.Tipo == dominio.TipoInmediato {
		primera := dominio.PrimeraFechaPosible(dominio.Hoy(ahora), dominio.AnticipacionDe(productos))
		fechas = make([]string, diasDeBusqueda)
		for dia := range fechas {
			fechas[dia] = dominio.SumarDias(primera, dia)
		}
	}

	id := servicio.nuevoID()
	for _, fecha := range fechas {
		pedido, err := dominio.NuevoPedido(id, comprador, clave, solicitud, productos, fecha, ahora)
		if err != nil {
			return dominio.Pedido{}, err
		}
		evento := dominio.NuevoEventoEstadoCambiado(servicio.nuevoID(), pedido, "", ahora)

		err = servicio.repositorio.Registrar(contexto, pedido, evento)
		if errors.Is(err, dominio.ErrSinCupo) {
			continue
		}
		if err != nil {
			if !errors.Is(err, dominio.ErrPedidoRepetido) {
				err = fmt.Errorf("registrar pedido: %w", err)
			}
			return dominio.Pedido{}, err
		}
		servicio.eventos.PedidoRegistrado(contexto, "aceptado")
		servicio.avisos.Avisar(evento)
		return pedido, nil
	}
	servicio.eventos.PedidoRegistrado(contexto, "sin_cupo")
	return dominio.Pedido{}, dominio.ErrSinCupo
}

// cambiarEstado aplica la transición y la guarda junto con su evento y, si se
// cancela, con la devolución del cupo y de la reserva de insumos. Cancelar no
// llama al proveedor: la reserva es propia (ADR-020).
func (servicio *Servicio) cambiarEstado(contexto context.Context, pedido *dominio.Pedido, nuevo dominio.Estado, motivo string, ahora time.Time) error {
	versionLeida := pedido.Version
	teniaReserva := pedido.InsumosReservados
	anterior, err := pedido.CambiarEstado(nuevo, motivo, ahora)
	if err != nil {
		return err
	}
	evento := dominio.NuevoEventoEstadoCambiado(servicio.nuevoID(), *pedido, anterior, ahora)
	cancelado := nuevo == dominio.EstadoCancelado
	return servicio.guardar(contexto, pedido, puertos.Cambio{
		Pedido:          *pedido,
		VersionAnterior: versionLeida,
		Evento:          &evento,
		LiberarCupo:     cancelado,
		LiberarReserva:  cancelado && teniaReserva,
	})
}

func (servicio *Servicio) guardar(contexto context.Context, pedido *dominio.Pedido, cambio puertos.Cambio) error {
	if err := servicio.repositorio.GuardarCambio(contexto, cambio); err != nil {
		if errors.Is(err, dominio.ErrModificadoAlMismoTiempo) {
			return err
		}
		return fmt.Errorf("guardar pedido %s: %w", pedido.ID, err)
	}
	pedido.Version = cambio.VersionAnterior + 1
	if cambio.Evento != nil {
		servicio.avisos.Avisar(*cambio.Evento)
	}
	return nil
}

// Obtener devuelve un pedido. Un comprador solo ve los suyos; para él, los de
// otro no existen.
func (servicio *Servicio) Obtener(contexto context.Context, idComprador, id string) (dominio.Pedido, error) {
	pedido, err := servicio.repositorio.Buscar(contexto, id)
	if err != nil {
		return dominio.Pedido{}, fmt.Errorf("buscar pedido %s: %w", id, err)
	}
	if idComprador != "" && !pedido.PerteneceA(idComprador) {
		return dominio.Pedido{}, dominio.ErrNoEncontrado
	}
	return pedido, nil
}

// ListarDeComprador devuelve los pedidos del comprador (C7).
func (servicio *Servicio) ListarDeComprador(contexto context.Context, idComprador string) ([]dominio.Pedido, error) {
	pedidos, err := servicio.repositorio.ListarDeComprador(contexto, idComprador)
	if err != nil {
		return nil, fmt.Errorf("listar pedidos del comprador: %w", err)
	}
	return pedidos, nil
}

// ListarDelDia devuelve lo que hay que producir en una fecha (VE4).
func (servicio *Servicio) ListarDelDia(contexto context.Context, fecha string) ([]dominio.Pedido, error) {
	if err := dominio.ValidarFecha(fecha); err != nil {
		return nil, err
	}
	pedidos, err := servicio.repositorio.ListarDelDia(contexto, fecha)
	if err != nil {
		return nil, fmt.Errorf("listar pedidos del día: %w", err)
	}
	return pedidos, nil
}

// Cancelar cancela un pedido del comprador antes de que entre en producción (C8,
// R6) y devuelve su cupo y su reserva de insumos (R7). También retira un pedido
// especial que todavía no se cotizó (ADR-017).
func (servicio *Servicio) Cancelar(contexto context.Context, idComprador, id string) (dominio.Pedido, error) {
	pedido, err := servicio.Obtener(contexto, idComprador, id)
	if err != nil {
		return dominio.Pedido{}, err
	}
	if pedido.Estado == dominio.EstadoSolicitado {
		return servicio.cancelarSolicitud(contexto, pedido, "Cancelado por el comprador.")
	}
	return servicio.aplicarEstado(contexto, pedido, dominio.EstadoCancelado, "Cancelado por el comprador.")
}

// CambiarEstado lo usa el vendedor para avanzar un pedido (VE6) o cancelarlo.
func (servicio *Servicio) CambiarEstado(contexto context.Context, id string, nuevo dominio.Estado) (dominio.Pedido, error) {
	pedido, err := servicio.Obtener(contexto, "", id)
	if err != nil {
		return dominio.Pedido{}, err
	}
	return servicio.aplicarEstado(contexto, pedido, nuevo, "Cancelado por la pastelería.")
}

func (servicio *Servicio) aplicarEstado(contexto context.Context, pedido dominio.Pedido, nuevo dominio.Estado, motivo string) (dominio.Pedido, error) {
	if err := servicio.cambiarEstado(contexto, &pedido, nuevo, motivo, servicio.ahora()); err != nil {
		return dominio.Pedido{}, err
	}
	return pedido, nil
}

// ListarPendientesDeConfirmar es lo que espera al vendedor (ADR-020), cada pedido
// con los insumos que hoy no alcanzan para hacerlo. Puede confirmarlo igual: decide él.
func (servicio *Servicio) ListarPendientesDeConfirmar(contexto context.Context) ([]dominio.PendienteDeConfirmar, error) {
	pedidos, err := servicio.repositorio.ListarPendientesDeConfirmar(contexto)
	if err != nil {
		return nil, fmt.Errorf("listar pedidos pendientes de confirmar: %w", err)
	}
	existencias, err := servicio.existencias.Listar(contexto)
	if err != nil {
		return nil, fmt.Errorf("listar existencias: %w", err)
	}
	porInsumo := make(map[[2]string]dominio.Existencia, len(existencias))
	for _, existencia := range existencias {
		porInsumo[[2]string{existencia.Ingrediente, existencia.Unidad}] = existencia
	}
	pendientes := make([]dominio.PendienteDeConfirmar, len(pedidos))
	for indice, pedido := range pedidos {
		pendientes[indice] = dominio.PendienteDeConfirmar{Pedido: pedido}
		for _, insumo := range pedido.Insumos {
			faltante := porInsumo[[2]string{insumo.Ingrediente, insumo.Unidad}].Faltante(insumo.Cantidad)
			if faltante > 0 {
				pendientes[indice].Faltantes = append(pendientes[indice].Faltantes, dominio.Insumo{Ingrediente: insumo.Ingrediente, Cantidad: faltante, Unidad: insumo.Unidad})
			}
		}
	}
	return pendientes, nil
}

// ListarStock devuelve el stock propio de cada ingrediente (ADR-020).
func (servicio *Servicio) ListarStock(contexto context.Context) ([]dominio.Existencia, error) {
	existencias, err := servicio.existencias.Listar(contexto)
	if err != nil {
		return nil, fmt.Errorf("listar existencias: %w", err)
	}
	return existencias, nil
}

// CargarStock suma cantidad al ingrediente en el proveedor y guarda el stock que
// este informa (ADR-020). Es la forma de cargar el stock inicial, que arranca en 0,
// y de reponerlo. Si el proveedor no responde, no se guarda nada y hay que repetirlo.
func (servicio *Servicio) CargarStock(contexto context.Context, ingrediente, unidad string, cantidad int64) (dominio.Existencia, error) {
	ingrediente, unidad = strings.TrimSpace(ingrediente), strings.TrimSpace(unidad)
	if ingrediente == "" || utf8.RuneCountInString(ingrediente) > largoMaximoIngrediente ||
		unidad == "" || utf8.RuneCountInString(unidad) > largoMaximoUnidad ||
		cantidad < 1 || cantidad > cantidadMaximaCarga {
		return dominio.Existencia{}, fmt.Errorf("%w: ingrediente, unidad y una cantidad entre 1 y %d son obligatorios", dominio.ErrSolicitudInvalida, cantidadMaximaCarga)
	}
	existencia, err := servicio.existencias.AsignarProducto(contexto, ingrediente, unidad)
	if err != nil {
		return dominio.Existencia{}, fmt.Errorf("asignar producto del proveedor: %w", err)
	}
	stock, err := servicio.inventario.Aumentar(contexto, existencia.ProductoID, cantidad, "Carga de stock de la pastelería")
	if err != nil {
		servicio.eventos.InventarioConsultado(contexto, "", resultadoDe(err), err)
		return dominio.Existencia{}, fmt.Errorf("aumentar el stock en el proveedor: %w", err)
	}
	servicio.eventos.InventarioConsultado(contexto, "", puertos.InventarioCargada, nil)
	if err := servicio.existencias.GuardarStock(contexto, ingrediente, unidad, stock); err != nil {
		return dominio.Existencia{}, fmt.Errorf("guardar el stock cargado: %w", err)
	}
	existencia.Stock = stock
	return existencia, nil
}

// resultadoDe resume una falla del proveedor para las métricas.
func resultadoDe(err error) puertos.ResultadoInventario {
	switch {
	case errors.Is(err, puertos.ErrStockInsuficiente):
		return puertos.InventarioSinStock
	case errors.Is(err, puertos.ErrInventarioRechazo):
		return puertos.InventarioRechazada
	case errors.Is(err, puertos.ErrInventarioNoDisponible):
		return puertos.InventarioNoDisponible
	default:
		return puertos.InventarioFallaTransitoria
	}
}

// DefinirCapacidad fija cuántas unidades se pueden producir un día (VE3).
func (servicio *Servicio) DefinirCapacidad(contexto context.Context, fecha string, capacidad int) (dominio.Dia, error) {
	if err := dominio.ValidarFecha(fecha); err != nil {
		return dominio.Dia{}, err
	}
	if capacidad < 0 {
		return dominio.Dia{}, fmt.Errorf("%w: la capacidad no puede ser negativa", dominio.ErrSolicitudInvalida)
	}
	dia, err := servicio.repositorio.DefinirCapacidad(contexto, fecha, capacidad)
	if err != nil && !errors.Is(err, dominio.ErrCapacidadMenorQueReservados) {
		return dominio.Dia{}, fmt.Errorf("definir capacidad del %s: %w", fecha, err)
	}
	return dia, err
}

// ListarCapacidad devuelve la capacidad y lo reservado de cada día del rango.
func (servicio *Servicio) ListarCapacidad(contexto context.Context, desde, hasta string) ([]dominio.Dia, error) {
	if err := errors.Join(dominio.ValidarFecha(desde), dominio.ValidarFecha(hasta)); err != nil {
		return nil, err
	}
	dias, err := servicio.repositorio.ListarCapacidad(contexto, desde, hasta)
	if err != nil {
		return nil, fmt.Errorf("listar capacidad: %w", err)
	}
	return dias, nil
}

// ProcesarConsumos descuenta en el proveedor los insumos de los pedidos que
// entraron en producción, de a un pedido por vez (ADR-020). Lo llama
// periódicamente un proceso en segundo plano y devuelve cuántos pedidos procesó.
func (servicio *Servicio) ProcesarConsumos(contexto context.Context) (int, error) {
	procesados := 0
	for {
		if err := contexto.Err(); err != nil {
			return procesados, err
		}
		ahora := servicio.ahora()
		pedido, encontrado, err := servicio.repositorio.TomarParaConsumir(contexto, ahora, ahora.Add(tiempoApartado))
		if err != nil {
			return procesados, fmt.Errorf("tomar pedido para consumir: %w", err)
		}
		if !encontrado {
			break
		}
		if err := servicio.consumir(contexto, &pedido); err != nil && !errors.Is(err, dominio.ErrModificadoAlMismoTiempo) {
			return procesados, err
		}
		procesados++
	}
	if pendientes, err := servicio.repositorio.ContarConsumosPendientes(contexto); err == nil {
		servicio.eventos.ConsumosPendientes(contexto, pendientes)
	}
	return procesados, nil
}

// consumir descuenta los insumos del pedido uno por uno y guarda el avance de cada
// uno, así que si algo falla a la mitad no se repite lo que ya se descontó. Se
// detiene en el primer insumo que no se pudo resolver.
func (servicio *Servicio) consumir(contexto context.Context, pedido *dominio.Pedido) error {
	for {
		insumo, hay := pedido.InsumoPorConsumir()
		if !hay {
			return nil
		}
		seguir, err := servicio.consumirInsumo(contexto, pedido, insumo)
		if err != nil || !seguir {
			return err
		}
	}
}

// consumirInsumo descuenta un insumo en el proveedor. Devuelve si se puede seguir
// con el siguiente.
//
// Reducir no es idempotente: un pedido repetido se descuenta dos veces. Por eso,
// antes de llamar, el pedido queda guardado "en duda"; si la llamada no responde o
// el servicio se cae a mitad, la próxima vez se consulta el stock del proveedor y
// se compara con el conocido para saber si ya se aplicó.
func (servicio *Servicio) consumirInsumo(contexto context.Context, pedido *dominio.Pedido, insumo dominio.Insumo) (bool, error) {
	existencia, err := servicio.existencias.AsignarProducto(contexto, insumo.Ingrediente, insumo.Unidad)
	if errors.Is(err, puertos.ErrSinProductoDelProveedor) {
		return false, servicio.pasarARevision(contexto, pedido, fmt.Sprintf("No hay un producto del proveedor para %s.", insumo.Ingrediente))
	}
	if err != nil {
		return false, fmt.Errorf("asignar producto del proveedor a %s: %w", insumo.Ingrediente, err)
	}

	enDudaAntes := pedido.ConsumoEnDuda
	if !enDudaAntes {
		pedido.ConsumoEnDuda = true
		if err := servicio.guardar(contexto, pedido, puertos.Cambio{Pedido: *pedido, VersionAnterior: pedido.Version}); err != nil {
			return false, err
		}
	}

	if enDudaAntes {
		yaAplicado, seguir, err := servicio.yaSeAplico(contexto, pedido, existencia, insumo)
		if err != nil || !seguir {
			return false, err
		}
		if yaAplicado != nil {
			return true, servicio.registrarConsumo(contexto, pedido, insumo, *yaAplicado)
		}
	}

	stock, err := servicio.inventario.Reducir(contexto, existencia.ProductoID, insumo.Cantidad, "Pedido "+pedido.ID)
	if err == nil {
		servicio.eventos.InventarioConsultado(contexto, pedido.ID, puertos.InventarioConsumida, nil)
		return true, servicio.registrarConsumo(contexto, pedido, insumo, stock)
	}

	servicio.eventos.InventarioConsultado(contexto, pedido.ID, resultadoDe(err), err)
	switch {
	case errors.Is(err, puertos.ErrStockInsuficiente):
		pedido.ConsumoEnDuda = false
		return false, servicio.pasarARevision(contexto, pedido, fmt.Sprintf("El proveedor no tiene stock de %s.", insumo.Ingrediente))
	case errors.Is(err, puertos.ErrInventarioRechazo):
		// Reintentar no arregla un error de formato: lo decide el vendedor y lo revisa el equipo.
		pedido.ConsumoEnDuda = false
		return false, servicio.pasarARevision(contexto, pedido, "El proveedor rechazó el consumo de insumos.")
	case errors.Is(err, puertos.ErrInventarioNoDisponible):
		// No se llegó a llamar: la duda es la de antes.
		pedido.ConsumoEnDuda = enDudaAntes
		pedido.RegistrarFalloDeConsumo(false, servicio.ahora())
	default:
		pedido.RegistrarFalloDeConsumo(true, servicio.ahora())
	}
	return false, servicio.guardar(contexto, pedido, puertos.Cambio{Pedido: *pedido, VersionAnterior: pedido.Version})
}

// yaSeAplico consulta el stock del proveedor después de una llamada sin respuesta.
// Si bajó exactamente lo que se quería descontar, devuelve el stock que quedó y no
// hay que repetir; si no cambió, devuelve nil y se repite. Si dio otra cosa no se
// puede saber qué pasó, y queda para el vendedor. seguir es false cuando ya se
// resolvió el pedido de otra forma (reintento o revisión).
func (servicio *Servicio) yaSeAplico(contexto context.Context, pedido *dominio.Pedido, existencia dominio.Existencia, insumo dominio.Insumo) (*int64, bool, error) {
	actual, err := servicio.inventario.Consultar(contexto, existencia.ProductoID)
	if err != nil {
		servicio.eventos.InventarioConsultado(contexto, pedido.ID, resultadoDe(err), err)
		pedido.RegistrarFalloDeConsumo(!errors.Is(err, puertos.ErrInventarioNoDisponible), servicio.ahora())
		return nil, false, servicio.guardar(contexto, pedido, puertos.Cambio{Pedido: *pedido, VersionAnterior: pedido.Version})
	}
	switch existencia.Stock - actual {
	case insumo.Cantidad:
		return &actual, true, nil
	case 0:
		return nil, true, nil
	}
	return nil, false, servicio.pasarARevision(contexto, pedido,
		fmt.Sprintf("El stock de %s en el proveedor (%d) no coincide con el que conocemos (%d).", insumo.Ingrediente, actual, existencia.Stock))
}

// registrarConsumo guarda el avance del pedido y el stock que quedó en el proveedor.
func (servicio *Servicio) registrarConsumo(contexto context.Context, pedido *dominio.Pedido, insumo dominio.Insumo, stockQueda int64) error {
	pedido.InsumoConsumido(servicio.ahora())
	return servicio.guardar(contexto, pedido, puertos.Cambio{
		Pedido:          *pedido,
		VersionAnterior: pedido.Version,
		Consumo:         &puertos.Consumo{Insumo: insumo, StockQueda: stockQueda},
	})
}

// pasarARevision deja el pedido para el vendedor. No frena la producción.
func (servicio *Servicio) pasarARevision(contexto context.Context, pedido *dominio.Pedido, motivo string) error {
	pedido.MarcarParaRevision(motivo, servicio.ahora())
	return servicio.guardar(contexto, pedido, puertos.Cambio{Pedido: *pedido, VersionAnterior: pedido.Version})
}

// PublicarEventos manda al broker los eventos de la bandeja de salida, del más
// viejo al más nuevo, y devuelve cuántos publicó. Si el broker no responde, corta:
// los eventos esperan guardados hasta que vuelva (ADR-011). El que falló se
// devuelve a la bandeja para que siga primero, así los eventos salen en orden
// (ADR-015).
func (servicio *Servicio) PublicarEventos(contexto context.Context) (int, error) {
	publicados := 0
	for {
		if err := contexto.Err(); err != nil {
			return publicados, err
		}
		ahora := servicio.ahora()
		guardado, encontrado, err := servicio.bandeja.TomarEvento(contexto, ahora, ahora.Add(tiempoApartadoEvento))
		if err != nil {
			return publicados, fmt.Errorf("tomar evento de la bandeja de salida: %w", err)
		}
		if !encontrado {
			return publicados, nil
		}
		if err := servicio.publicador.Publicar(contexto, guardado); err != nil {
			servicio.eventos.EventoPublicado(contexto, guardado.Evento.ID, err)
			// Si devolverlo falla, el evento sale igual cuando vence el apartado.
			if err := servicio.bandeja.DevolverEvento(contexto, guardado.Evento.ID, ahora); err != nil {
				return publicados, fmt.Errorf("devolver evento %s a la bandeja de salida: %w", guardado.Evento.ID, err)
			}
			return publicados, nil
		}
		// Si esto falla, el evento se vuelve a publicar cuando vence el apartado: el
		// consumidor descarta los repetidos por idEvento (entrega "al menos una vez").
		if err := servicio.bandeja.MarcarPublicado(contexto, guardado.Evento.ID); err != nil {
			return publicados, fmt.Errorf("marcar evento %s como publicado: %w", guardado.Evento.ID, err)
		}
		servicio.eventos.EventoPublicado(contexto, guardado.Evento.ID, nil)
		publicados++
	}
}

func validarClave(clave string) error {
	largo := utf8.RuneCountInString(clave)
	if largo == 0 || largo > largoMaximoClave {
		return fmt.Errorf("%w: la Idempotency-Key tiene que tener entre 1 y %d caracteres", dominio.ErrSolicitudInvalida, largoMaximoClave)
	}
	return nil
}
