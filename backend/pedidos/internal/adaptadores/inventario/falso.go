// Package inventario tiene los adaptadores del puerto ProveedorDeStock: el que llama
// al inventario del otro grupo (ADR-020), un inventario falso configurable para el
// desarrollo, los tests y las caídas provocadas, y el disyuntor que protege a
// cualquiera de ellos (ADR-011).
package inventario

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

// Modo de respuesta del inventario falso, para la demo y las caídas provocadas.
type Modo string

const (
	ModoConfirma Modo = "confirma"  // lleva el stock como un proveedor real
	ModoSinStock Modo = "sin_stock" // reducir siempre responde que no alcanza
	ModoLento    Modo = "lento"     // tarda más que el timeout
	ModoError    Modo = "error"     // falla como un 5xx
	ModoRechazo  Modo = "rechazo"   // falla como un 4xx inesperado
)

// ModoValido indica si el texto es uno de los modos.
func ModoValido(texto string) bool {
	switch Modo(texto) {
	case ModoConfirma, ModoSinStock, ModoLento, ModoError, ModoRechazo:
		return true
	}
	return false
}

// TiempoMaximo es el timeout de cada llamada al inventario (ADR-020).
const TiempoMaximo = 1500 * time.Millisecond

// demoraLenta es cuánto tarda en modo lento: más que TiempoMaximo, el timeout del Disyuntor.
const demoraLenta = 5 * time.Second

// Falso cumple puertos.ProveedorDeStock sin llamar a nadie. Lleva el stock en
// memoria: arranca en cero, igual que el del proveedor real.
type Falso struct {
	modo   Modo
	mutex  sync.Mutex
	stocks map[int64]int64
}

var _ puertos.ProveedorDeStock = (*Falso)(nil)

// NuevoFalso responde según el modo.
func NuevoFalso(modo Modo) *Falso {
	return &Falso{modo: modo, stocks: map[int64]int64{}}
}

// Reducir resta del stock del producto, o responde que no alcanza.
func (falso *Falso) Reducir(contexto context.Context, idProducto, cantidad int64, _ string) (int64, error) {
	if err := falso.responder(contexto); err != nil {
		return 0, err
	}
	falso.mutex.Lock()
	defer falso.mutex.Unlock()
	if falso.modo == ModoSinStock || falso.stocks[idProducto] < cantidad {
		return 0, fmt.Errorf("%w: el falso tiene %d y se pidieron %d", puertos.ErrStockInsuficiente, falso.stocks[idProducto], cantidad)
	}
	falso.stocks[idProducto] -= cantidad
	return falso.stocks[idProducto], nil
}

// Aumentar suma al stock del producto.
func (falso *Falso) Aumentar(contexto context.Context, idProducto, cantidad int64, _ string) (int64, error) {
	if err := falso.responder(contexto); err != nil {
		return 0, err
	}
	falso.mutex.Lock()
	defer falso.mutex.Unlock()
	falso.stocks[idProducto] += cantidad
	return falso.stocks[idProducto], nil
}

// Consultar devuelve el stock del producto.
func (falso *Falso) Consultar(contexto context.Context, idProducto int64) (int64, error) {
	if err := falso.responder(contexto); err != nil {
		return 0, err
	}
	falso.mutex.Lock()
	defer falso.mutex.Unlock()
	return falso.stocks[idProducto], nil
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
