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
	fallar  error // si no es nil, todas las escrituras fallan con este error
}

func nuevoRepositorioFalso() *repositorioFalso {
	return &repositorioFalso{pedidos: map[string]dominio.Pedido{}, dias: map[string]dominio.Dia{}}
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
	dia := repositorio.dias[pedido.Fecha]
	if dia.Reservados+pedido.Unidades() > dia.Capacidad {
		return dominio.ErrSinCupo
	}
	dia.Reservados += pedido.Unidades()
	repositorio.dias[pedido.Fecha] = dia
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
	if repositorio.pedidos[cambio.Pedido.ID].Version != cambio.VersionAnterior {
		return dominio.ErrModificadoAlMismoTiempo
	}
	pedido := cambio.Pedido
	pedido.Version = cambio.VersionAnterior + 1
	repositorio.pedidos[pedido.ID] = pedido
	if cambio.LiberarCupo {
		dia := repositorio.dias[pedido.Fecha]
		dia.Reservados -= pedido.Unidades()
		repositorio.dias[pedido.Fecha] = dia
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

func (repositorio *repositorioFalso) TomarParaConfirmar(_ context.Context, ahora, hasta time.Time) (dominio.Pedido, bool, error) {
	return repositorio.tomar(func(pedido *dominio.Pedido) bool {
		if pedido.Estado != dominio.EstadoPendienteConfirmacion || pedido.RequiereRevision || pedido.ProximaConfirmacion.After(ahora) {
			return false
		}
		pedido.ProximaConfirmacion = hasta
		return true
	})
}

func (repositorio *repositorioFalso) TomarParaLiberar(_ context.Context, ahora, hasta time.Time) (dominio.Pedido, bool, error) {
	return repositorio.tomar(func(pedido *dominio.Pedido) bool {
		if !pedido.LiberarInsumos || pedido.ProximaLiberacion.After(ahora) {
			return false
		}
		pedido.ProximaLiberacion = hasta
		return true
	})
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
	if len(repositorio.eventos) == 0 {
		return puertos.EventoGuardado{}, false, nil
	}
	return repositorio.eventos[0], true, nil
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

// inventarioFalso responde lo configurado y cuenta las llamadas.
type inventarioFalso struct {
	mutex       sync.Mutex
	resultado   puertos.ResultadoReserva
	fallar      error
	reservas    int
	liberados   []string
	fallarLiber error
}

func (inventario *inventarioFalso) Reservar(_ context.Context, idPedido string, _ []dominio.Insumo) (puertos.ResultadoReserva, error) {
	inventario.mutex.Lock()
	defer inventario.mutex.Unlock()
	inventario.reservas++
	if inventario.fallar != nil {
		return "", inventario.fallar
	}
	return inventario.resultado, nil
}

func (inventario *inventarioFalso) Liberar(_ context.Context, idPedido string) error {
	inventario.mutex.Lock()
	defer inventario.mutex.Unlock()
	if inventario.fallarLiber != nil {
		return inventario.fallarLiber
	}
	inventario.liberados = append(inventario.liberados, idPedido)
	return nil
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

var errCaido = errors.New("caído")
