package inventario

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sony/gobreaker/v2"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

// Configuracion del disyuntor.
type Configuracion struct {
	FallosParaAbrir uint32
	EsperaAbierto   time.Duration
}

// ConfiguracionDelServicio son los valores de ADR-020 y ADR-011: 5 fallas
// seguidas lo abren por 30 s.
var ConfiguracionDelServicio = Configuracion{FallosParaAbrir: 5, EsperaAbierto: 30 * time.Second}

// Disyuntor envuelve a otro ProveedorDeStock: con el inventario caído no lo llama
// y devuelve puertos.ErrInventarioNoDisponible al instante, así el consumo espera
// sin gastar el timeout en cada insumo.
type Disyuntor struct {
	interno  puertos.ProveedorDeStock
	circuito *gobreaker.CircuitBreaker[int64]
}

var _ puertos.ProveedorDeStock = (*Disyuntor)(nil)

// NuevoDisyuntor envuelve a interno. Cada cambio de estado se avisa por Eventos.
func NuevoDisyuntor(interno puertos.ProveedorDeStock, eventos puertos.Eventos, configuracion Configuracion) *Disyuntor {
	circuito := gobreaker.NewCircuitBreaker[int64](gobreaker.Settings{
		Name:        "inventario",
		MaxRequests: 1,
		Timeout:     configuracion.EsperaAbierto,
		ReadyToTrip: func(cuentas gobreaker.Counts) bool {
			return cuentas.ConsecutiveFailures >= configuracion.FallosParaAbrir
		},
		// Un 409 o un rechazo por formato no son una caída: el proveedor respondió.
		IsSuccessful: func(err error) bool {
			return err == nil || errors.Is(err, puertos.ErrInventarioRechazo) || errors.Is(err, puertos.ErrStockInsuficiente)
		},
		IsExcluded: func(err error) bool {
			return errors.Is(err, context.Canceled)
		},
		OnStateChange: func(_ string, anterior, nuevo gobreaker.State) {
			eventos.CircuitoDelInventarioCambio(context.Background(), aEstado(anterior), aEstado(nuevo))
		},
	})
	return &Disyuntor{interno: interno, circuito: circuito}
}

// Reducir pasa la llamada al inventario si el circuito lo permite, con el timeout de ADR-020.
func (disyuntor *Disyuntor) Reducir(contexto context.Context, idProducto, cantidad int64, motivo string) (int64, error) {
	return disyuntor.ejecutar(contexto, func(contexto context.Context) (int64, error) {
		return disyuntor.interno.Reducir(contexto, idProducto, cantidad, motivo)
	})
}

// Aumentar pasa la llamada al inventario si el circuito lo permite.
func (disyuntor *Disyuntor) Aumentar(contexto context.Context, idProducto, cantidad int64, motivo string) (int64, error) {
	return disyuntor.ejecutar(contexto, func(contexto context.Context) (int64, error) {
		return disyuntor.interno.Aumentar(contexto, idProducto, cantidad, motivo)
	})
}

// Consultar pasa la llamada al inventario si el circuito lo permite.
func (disyuntor *Disyuntor) Consultar(contexto context.Context, idProducto int64) (int64, error) {
	return disyuntor.ejecutar(contexto, func(contexto context.Context) (int64, error) {
		return disyuntor.interno.Consultar(contexto, idProducto)
	})
}

func (disyuntor *Disyuntor) ejecutar(contexto context.Context, llamada func(contexto context.Context) (int64, error)) (int64, error) {
	stock, err := disyuntor.circuito.Execute(func() (int64, error) {
		contexto, cancelar := context.WithTimeout(contexto, TiempoMaximo)
		defer cancelar()
		return llamada(contexto)
	})
	return stock, disyuntor.traducir(err)
}

func (disyuntor *Disyuntor) traducir(err error) error {
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		return fmt.Errorf("%w: el disyuntor está %s", puertos.ErrInventarioNoDisponible, aEstado(disyuntor.circuito.State()))
	}
	return err
}

func aEstado(estado gobreaker.State) puertos.EstadoCircuito {
	switch estado {
	case gobreaker.StateOpen:
		return puertos.CircuitoAbierto
	case gobreaker.StateHalfOpen:
		return puertos.CircuitoSemiabierto
	default:
		return puertos.CircuitoCerrado
	}
}
