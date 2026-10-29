// Package inventario tiene los adaptadores del puerto ReservadorDeInsumos: un
// inventario falso configurable, que se usa mientras el grupo proveedor no publica
// su contrato (ADR-010), y el disyuntor que protege a cualquiera de ellos (ADR-011).
package inventario

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

// Modo de respuesta del inventario falso, para la demo y las caídas provocadas.
type Modo string

const (
	ModoConfirma Modo = "confirma" // reserva todo
	ModoSinStock Modo = "sin_stock"
	ModoLento    Modo = "lento"   // tarda más que el timeout
	ModoError    Modo = "error"   // falla como un 5xx
	ModoRechazo  Modo = "rechazo" // falla como un 4xx inesperado
)

// ModoValido indica si el texto es uno de los modos.
func ModoValido(texto string) bool {
	switch Modo(texto) {
	case ModoConfirma, ModoSinStock, ModoLento, ModoError, ModoRechazo:
		return true
	}
	return false
}

// TiempoMaximo es el timeout de cada llamada al inventario (ADR-010).
const TiempoMaximo = 1500 * time.Millisecond

// demoraLenta es cuánto tarda en modo lento: más que TiempoMaximo, el timeout del Disyuntor.
const demoraLenta = 5 * time.Second

// Falso cumple puertos.ReservadorDeInsumos sin llamar a nadie.
type Falso struct {
	modo Modo
}

var _ puertos.ReservadorDeInsumos = (*Falso)(nil)

// NuevoFalso responde siempre según el modo.
func NuevoFalso(modo Modo) *Falso {
	return &Falso{modo: modo}
}

// Reservar simula la reserva de insumos.
func (falso *Falso) Reservar(contexto context.Context, idPedido string, insumos []dominio.Insumo) (puertos.ResultadoReserva, error) {
	if err := falso.responder(contexto); err != nil {
		return "", err
	}
	if falso.modo == ModoSinStock {
		return puertos.ReservaSinStock, nil
	}
	return puertos.ReservaConfirmada, nil
}

// Liberar simula la liberación.
func (falso *Falso) Liberar(contexto context.Context, idPedido string) error {
	return falso.responder(contexto)
}

func (falso *Falso) responder(contexto context.Context) error {
	switch falso.modo {
	case ModoLento:
		// El timeout lo pone el Disyuntor, como con un inventario real.
		select {
		case <-contexto.Done():
			return fmt.Errorf("el inventario no respondió a tiempo: %w", contexto.Err())
		case <-time.After(demoraLenta):
			return nil
		}
	case ModoError:
		return errors.New("el inventario respondió 503")
	case ModoRechazo:
		return fmt.Errorf("%w: respondió 422", puertos.ErrInventarioRechazo)
	}
	return nil
}
