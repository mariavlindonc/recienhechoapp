package aplicacion_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

// repositorioFalso guarda todo en memoria y respeta las mismas reglas que el de
// MongoDB: cupo condicional, clave única por comprador y versión.
type repositorioFalso struct {
	mutex   sync.Mutex
	pedidos map[string]dominio.Pedido
	dias    map[string]dominio.Dia
	eventos []puertos.EventoGuardado
	// apartados guarda, por idEvento, hasta cuándo está apartado un evento.
	apartados map[string]time.Time
	fallar    error // si no es nil, todas las escrituras fallan con este error
	// fallarGuardar, si no es nil, hace fallar solo GuardarCambio: el pedido se
	// registra, pero no se guarda lo que pasa después.
	fallarGuardar error
	// existencias comparte el stock con existenciasFalsas: Registrar reserva, y
	// GuardarCambio devuelve la reserva o registra el consumo, como el de MongoDB.
	existencias *existenciasFalsas
}

func nuevoRepositorioFalso(existencias *existenciasFalsas) *repositorioFalso {
	return &repositorioFalso{pedidos: map[string]dominio.Pedido{}, dias: map[string]dominio.Dia{}, apartados: map[string]time.Time{}, existencias: existencias}
}

func (repositorio *repositorioFalso) Registrar(_ context.Context, pedido dominio.Pedido, evento dominio.Evento) error {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	if repositorio.fallar != nil {
		return repositorio.fallar
	}
	for _, existente := range repositorio.pedidos {
		if existente.Comprador.ID == pedido.Comprador.ID && existente.ClaveIdempotencia == pedido.ClaveIdempotencia {
			return dominio.ErrPedidoRepetido
		}
	}
	if !pedido.EsperaCotizacion() {
		if err := repositorio.reservarCupo(pedido); err != nil {
			return err
		}
	}
	if pedido.InsumosReservados {
		repositorio.existencias.mover(pedido.Insumos, 1)
	}
	repositorio.pedidos[pedido.ID] = pedido
	repositorio.eventos = append(repositorio.eventos, puertos.EventoGuardado{Evento: evento})
	return nil
}

func (repositorio *repositorioFalso) GuardarCambio(_ context.Context, cambio puertos.Cambio) error {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	if repositorio.fallar != nil {
		return repositorio.fallar
	}
	if repositorio.fallarGuardar != nil {
		return repositorio.fallarGuardar
	}
	if repositorio.pedidos[cambio.Pedido.ID].Version != cambio.VersionAnterior {
		return dominio.ErrModificadoAlMismoTiempo
	}
	pedido := cambio.Pedido
	if cambio.ReservarCupo {
		if err := repositorio.reservarCupo(pedido); err != nil {
			return err
		}
	}
	pedido.Version = cambio.VersionAnterior + 1
	repositorio.pedidos[pedido.ID] = pedido
	if cambio.LiberarCupo {
		dia := repositorio.dias[pedido.Fecha]
		dia.Reservados -= pedido.Unidades()
		repositorio.dias[pedido.Fecha] = dia
	}
	if cambio.LiberarReserva {
		repositorio.existencias.mover(pedido.Insumos[pedido.InsumosConsumidos:], -1)
	}
	if cambio.Consumo != nil {
		repositorio.existencias.consumir(cambio.Consumo.Insumo, cambio.Consumo.StockQueda)
	}
	if cambio.Evento != nil {
		repositorio.eventos = append(repositorio.eventos, puertos.EventoGuardado{Evento: *cambio.Evento})
	}
	return nil
}

func (repositorio *repositorioFalso) Buscar(_ context.Context, id string) (dominio.Pedido, error) {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	pedido, existe := repositorio.pedidos[id]
	if !existe {
		return dominio.Pedido{}, dominio.ErrNoEncontrado
	}
	return pedido, nil
}

func (repositorio *repositorioFalso) BuscarPorClave(_ context.Context, idComprador, clave string) (dominio.Pedido, error) {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	for _, pedido := range repositorio.pedidos {
		if pedido.Comprador.ID == idComprador && pedido.ClaveIdempotencia == clave {
			return pedido, nil
		}
	}
	return dominio.Pedido{}, dominio.ErrNoEncontrado
}

func (repositorio *repositorioFalso) ListarDeComprador(_ context.Context, idComprador string) ([]dominio.Pedido, error) {
	return repositorio.filtrar(func(pedido dominio.Pedido) bool { return pedido.Comprador.ID == idComprador }), nil
}

func (repositorio *repositorioFalso) ListarDelDia(_ context.Context, fecha string) ([]dominio.Pedido, error) {
	return repositorio.filtrar(func(pedido dominio.Pedido) bool { return pedido.Fecha == fecha }), nil
}

func (repositorio *repositorioFalso) ListarEspecialesAbiertos(context.Context) ([]dominio.Pedido, error) {
	return repositorio.filtrar(func(pedido dominio.Pedido) bool {
		return pedido.EsperaCotizacion()
	}), nil
}

// reservarCupo se llama con el mutex tomado.
func (repositorio *repositorioFalso) reservarCupo(pedido dominio.Pedido) error {
	dia := repositorio.dias[pedido.Fecha]
	if dia.Reservados+pedido.Unidades() > dia.Capacidad {
		return dominio.ErrSinCupo
	}
	dia.Reservados += pedido.Unidades()
	repositorio.dias[pedido.Fecha] = dia
	return nil
}

func (repositorio *repositorioFalso) filtrar(incluir func(dominio.Pedido) bool) []dominio.Pedido {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	var encontrados []dominio.Pedido
	for _, pedido := range repositorio.pedidos {
		if incluir(pedido) {
			encontrados = append(encontrados, pedido)
		}
	}
	sort.Slice(encontrados, func(primero, segundo int) bool { return encontrados[primero].ID < encontrados[segundo].ID })
	return encontrados
}

func (repositorio *repositorioFalso) ListarPendientesDeConfirmar(context.Context) ([]dominio.Pedido, error) {
	return repositorio.filtrar(func(pedido dominio.Pedido) bool { return pedido.Estado == dominio.EstadoPendienteConfirmacion }), nil
}

func (repositorio *repositorioFalso) TomarParaConsumir(_ context.Context, ahora, hasta time.Time) (dominio.Pedido, bool, error) {
	return repositorio.tomar(func(pedido *dominio.Pedido) bool {
		if !pedido.ConsumoPendiente || pedido.ProximoConsumo.After(ahora) {
			return false
		}
		pedido.ProximoConsumo = hasta
		return true
	})
}

func (repositorio *repositorioFalso) ContarConsumosPendientes(context.Context) (int, error) {
	return len(repositorio.filtrar(func(pedido dominio.Pedido) bool { return pedido.ConsumoPendiente })), nil
}

func (repositorio *repositorioFalso) tomar(apartar func(*dominio.Pedido) bool) (dominio.Pedido, bool, error) {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	for id, pedido := range repositorio.pedidos {
		if apartar(&pedido) {
			pedido.Version++
			repositorio.pedidos[id] = pedido
			return pedido, true, nil
		}
	}
	return dominio.Pedido{}, false, nil
}

func (repositorio *repositorioFalso) DefinirCapacidad(_ context.Context, fecha string, capacidad int) (dominio.Dia, error) {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	dia := repositorio.dias[fecha]
	dia.Fecha = fecha
	if dia.Reservados > capacidad {
		return dia, dominio.ErrCapacidadMenorQueReservados
	}
	dia.Capacidad = capacidad
	repositorio.dias[fecha] = dia
	return dia, nil
}

func (repositorio *repositorioFalso) ListarCapacidad(_ context.Context, desde, hasta string) ([]dominio.Dia, error) {
	return nil, nil
}

func (repositorio *repositorioFalso) TomarEvento(_ context.Context, ahora, hasta time.Time) (puertos.EventoGuardado, bool, error) {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	for _, guardado := range repositorio.eventos {
		if repositorio.apartados[guardado.Evento.ID].After(ahora) {
			continue
		}
		repositorio.apartados[guardado.Evento.ID] = hasta
		return guardado, true, nil
	}
	return puertos.EventoGuardado{}, false, nil
}

func (repositorio *repositorioFalso) DevolverEvento(_ context.Context, idEvento string, ahora time.Time) error {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	repositorio.apartados[idEvento] = ahora
	return nil
}

func (repositorio *repositorioFalso) MarcarPublicado(_ context.Context, idEvento string) error {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	for indice, guardado := range repositorio.eventos {
		if guardado.Evento.ID == idEvento {
			repositorio.eventos = append(repositorio.eventos[:indice], repositorio.eventos[indice+1:]...)
			return nil
		}
	}
	return fmt.Errorf("evento %s inexistente", idEvento)
}

func (repositorio *repositorioFalso) reservados(fecha string) int {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	return repositorio.dias[fecha].Reservados
}

func (repositorio *repositorioFalso) estadosDeLosEventos() []string {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	var estados []string
	for _, guardado := range repositorio.eventos {
		estados = append(estados, guardado.Evento.Datos.EstadoNuevo)
	}
	return estados
}

// catalogoFalso devuelve siempre los mismos productos.
type catalogoFalso struct {
	productos map[string]dominio.Producto
	fallar    error
}

func (catalogo catalogoFalso) ObtenerProducto(_ context.Context, id string) (dominio.Producto, error) {
	if catalogo.fallar != nil {
		return dominio.Producto{}, catalogo.fallar
	}
	producto, existe := catalogo.productos[id]
	if !existe {
		return dominio.Producto{}, dominio.ErrProductoInexistente
	}
	return producto, nil
}

// existenciasFalsas guarda el stock propio en memoria y respeta las mismas reglas
// que el de MongoDB: la reserva crea la existencia en cero y los productos del
// proveedor se reparten de a uno por ingrediente.
type existenciasFalsas struct {
	mutex     sync.Mutex
	porInsumo map[[2]string]*dominio.Existencia
	productos []int64 // casilleros que quedan por repartir
}

func nuevasExistenciasFalsas(productos ...int64) *existenciasFalsas {
	return &existenciasFalsas{porInsumo: map[[2]string]*dominio.Existencia{}, productos: productos}
}

// existencia se llama con el mutex tomado.
func (existencias *existenciasFalsas) existencia(ingrediente, unidad string) *dominio.Existencia {
	clave := [2]string{ingrediente, unidad}
	if existencias.porInsumo[clave] == nil {
		existencias.porInsumo[clave] = &dominio.Existencia{Ingrediente: ingrediente, Unidad: unidad}
	}
	return existencias.porInsumo[clave]
}

func (existencias *existenciasFalsas) mover(insumos []dominio.Insumo, sentido int64) {
	existencias.mutex.Lock()
	defer existencias.mutex.Unlock()
	for _, insumo := range insumos {
		existencias.existencia(insumo.Ingrediente, insumo.Unidad).Reservado += sentido * insumo.Cantidad
	}
}

func (existencias *existenciasFalsas) consumir(insumo dominio.Insumo, stockQueda int64) {
	existencias.mutex.Lock()
	defer existencias.mutex.Unlock()
	actual := existencias.existencia(insumo.Ingrediente, insumo.Unidad)
	actual.Stock = stockQueda
	actual.Reservado -= insumo.Cantidad
}

// conStock deja cargado el stock conocido de un ingrediente.
func (existencias *existenciasFalsas) conStock(ingrediente, unidad string, stock int64) {
	existencias.mutex.Lock()
	defer existencias.mutex.Unlock()
	existencias.existencia(ingrediente, unidad).Stock = stock
}

func (existencias *existenciasFalsas) de(ingrediente, unidad string) dominio.Existencia {
	existencias.mutex.Lock()
	defer existencias.mutex.Unlock()
	return *existencias.existencia(ingrediente, unidad)
}

func (existencias *existenciasFalsas) Listar(context.Context) ([]dominio.Existencia, error) {
	existencias.mutex.Lock()
	defer existencias.mutex.Unlock()
	var todas []dominio.Existencia
	for _, existencia := range existencias.porInsumo {
		todas = append(todas, *existencia)
	}
	sort.Slice(todas, func(primera, segunda int) bool { return todas[primera].Ingrediente < todas[segunda].Ingrediente })
	return todas, nil
}

func (existencias *existenciasFalsas) Obtener(_ context.Context, ingrediente, unidad string) (dominio.Existencia, error) {
	existencias.mutex.Lock()
	defer existencias.mutex.Unlock()
	return *existencias.existencia(ingrediente, unidad), nil
}

func (existencias *existenciasFalsas) AsignarProducto(_ context.Context, ingrediente, unidad string) (dominio.Existencia, error) {
	existencias.mutex.Lock()
	defer existencias.mutex.Unlock()
	actual := existencias.existencia(ingrediente, unidad)
	if actual.ProductoID != 0 {
		return *actual, nil
	}
	if len(existencias.productos) == 0 {
		return dominio.Existencia{}, fmt.Errorf("%w: %s", puertos.ErrSinProductoDelProveedor, ingrediente)
	}
	actual.ProductoID, existencias.productos = existencias.productos[0], existencias.productos[1:]
	return *actual, nil
}

func (existencias *existenciasFalsas) GuardarStock(_ context.Context, ingrediente, unidad string, stock int64) error {
	existencias.conStock(ingrediente, unidad, stock)
	return nil
}

// proveedorFalso lleva el stock del proveedor por producto y cuenta las llamadas.
// Responde con la falla que se le indique y puede aplicar el cambio igual (como un
// proveedor al que se le cortó la respuesta).
type proveedorFalso struct {
	mutex  sync.Mutex
	stocks map[int64]int64
	// fallar, si no es nil, es lo que devuelve en vez de responder.
	fallar error
	// aplicarAunqueFalle hace que Reducir descuente el stock y después devuelva el
	// error de fallar: la respuesta se perdió pero el cambio ocurrió.
	aplicarAunqueFalle bool
	reducciones        []llamada
	consultas          int
}

type llamada struct {
	producto, cantidad int64
	motivo             string
}

func nuevoProveedorFalso() *proveedorFalso {
	return &proveedorFalso{stocks: map[int64]int64{}}
}

func (proveedor *proveedorFalso) Reducir(_ context.Context, idProducto, cantidad int64, motivo string) (int64, error) {
	proveedor.mutex.Lock()
	defer proveedor.mutex.Unlock()
	proveedor.reducciones = append(proveedor.reducciones, llamada{idProducto, cantidad, motivo})
	if proveedor.fallar != nil && !proveedor.aplicarAunqueFalle {
		return 0, proveedor.fallar
	}
	if proveedor.stocks[idProducto] < cantidad {
		return 0, puertos.ErrStockInsuficiente
	}
	proveedor.stocks[idProducto] -= cantidad
	if proveedor.fallar != nil {
		return 0, proveedor.fallar
	}
	return proveedor.stocks[idProducto], nil
}

func (proveedor *proveedorFalso) Aumentar(_ context.Context, idProducto, cantidad int64, _ string) (int64, error) {
	proveedor.mutex.Lock()
	defer proveedor.mutex.Unlock()
	if proveedor.fallar != nil {
		return 0, proveedor.fallar
	}
	proveedor.stocks[idProducto] += cantidad
	return proveedor.stocks[idProducto], nil
}

func (proveedor *proveedorFalso) Consultar(_ context.Context, idProducto int64) (int64, error) {
	proveedor.mutex.Lock()
	defer proveedor.mutex.Unlock()
	proveedor.consultas++
	if proveedor.fallar != nil && !proveedor.aplicarAunqueFalle {
		return 0, proveedor.fallar
	}
	return proveedor.stocks[idProducto], nil
}

// publicadorFalso guarda lo publicado o falla.
type publicadorFalso struct {
	publicados []string
	fallar     error
}

func (publicador *publicadorFalso) Publicar(_ context.Context, guardado puertos.EventoGuardado) error {
	if publicador.fallar != nil {
		return publicador.fallar
	}
	publicador.publicados = append(publicador.publicados, guardado.Evento.ID)
	return nil
}

// eventosFalsos registra los hechos avisados.
type eventosFalsos struct {
	mutex      sync.Mutex
	inventario []puertos.ResultadoInventario
	pendientes int
}

func (eventos *eventosFalsos) PedidoRegistrado(context.Context, string) {}
func (eventos *eventosFalsos) InventarioConsultado(_ context.Context, _ string, resultado puertos.ResultadoInventario, _ error) {
	eventos.mutex.Lock()
	defer eventos.mutex.Unlock()
	eventos.inventario = append(eventos.inventario, resultado)
}
func (eventos *eventosFalsos) CircuitoDelInventarioCambio(context.Context, puertos.EstadoCircuito, puertos.EstadoCircuito) {
}
func (eventos *eventosFalsos) EventoPublicado(context.Context, string, error) {}
func (eventos *eventosFalsos) ConsumosPendientes(_ context.Context, cantidad int) {
	eventos.mutex.Lock()
	defer eventos.mutex.Unlock()
	eventos.pendientes = cantidad
}

var errCaido = errors.New("caído")

// avisosFalsos anota lo que se les avisaría a los compradores conectados.
type avisosFalsos struct {
	mutex   sync.Mutex
	eventos []dominio.Evento
}

func (avisos *avisosFalsos) Avisar(evento dominio.Evento) {
	avisos.mutex.Lock()
	defer avisos.mutex.Unlock()
	avisos.eventos = append(avisos.eventos, evento)
}

func (avisos *avisosFalsos) estados() []string {
	avisos.mutex.Lock()
	defer avisos.mutex.Unlock()
	estados := make([]string, len(avisos.eventos))
	for indice, evento := range avisos.eventos {
		estados[indice] = evento.Datos.EstadoNuevo
	}
	return estados
}
