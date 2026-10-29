package inventario

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sony/gobreaker/v2"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

// Configuracion del disyuntor.
type Configuracion struct {
	FallosParaAbrir uint32
	EsperaAbierto   time.Duration
}

// ConfiguracionDelServicio son los valores de ADR-010 y ADR-011: 5 fallas
// seguidas lo abren por 30 s.
var ConfiguracionDelServicio = Configuracion{FallosParaAbrir: 5, EsperaAbierto: 30 * time.Second}

// Disyuntor envuelve a otro ReservadorDeInsumos: con el inventario caído no lo
// llama y devuelve puertos.ErrInventarioNoDisponible al instante, así los pedidos
// quedan pendiente_confirmacion sin esperar el timeout.
type Disyuntor struct {
	interno  puertos.ReservadorDeInsumos
	circuito *gobreaker.CircuitBreaker[puertos.ResultadoReserva]
}

var _ puertos.ReservadorDeInsumos = (*Disyuntor)(nil)

// NuevoDisyuntor envuelve a interno. Cada cambio de estado se avisa por Eventos.
func NuevoDisyuntor(interno puertos.ReservadorDeInsumos, eventos puertos.Eventos, configuracion Configuracion) *Disyuntor {
	circuito := gobreaker.NewCircuitBreaker[puertos.ResultadoReserva](gobreaker.Settings{
		Name:        "inventario",
		MaxRequests: 1,
		Timeout:     configuracion.EsperaAbierto,
		ReadyToTrip: func(cuentas gobreaker.Counts) bool {
			return cuentas.ConsecutiveFailures >= configuracion.FallosParaAbrir
		},
		// Un rechazo por formato no es una caída: el proveedor respondió.
		IsSuccessful: func(err error) bool {
			return err == nil || errors.Is(err, puertos.ErrInventarioRechazo)
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

// Reservar pasa la reserva al inventario si el circuito lo permite, con el timeout de ADR-010.
func (disyuntor *Disyuntor) Reservar(contexto context.Context, idPedido string, insumos []dominio.Insumo) (puertos.ResultadoReserva, error) {
	resultado, err := disyuntor.circuito.Execute(func() (puertos.ResultadoReserva, error) {
		contexto, cancelar := context.WithTimeout(contexto, TiempoMaximo)
		defer cancelar()
		return disyuntor.interno.Reservar(contexto, idPedido, insumos)
	})
	return resultado, disyuntor.traducir(err)
}

// Liberar pasa la liberación al inventario si el circuito lo permite.
func (disyuntor *Disyuntor) Liberar(contexto context.Context, idPedido string) error {
	_, err := disyuntor.circuito.Execute(func() (puertos.ResultadoReserva, error) {
		contexto, cancelar := context.WithTimeout(contexto, TiempoMaximo)
		defer cancelar()
		return "", disyuntor.interno.Liberar(contexto, idPedido)
	})
	return disyuntor.traducir(err)
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
