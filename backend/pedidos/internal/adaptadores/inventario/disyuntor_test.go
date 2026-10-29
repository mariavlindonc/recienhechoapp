package inventario_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/adaptadores/inventario"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

type eventosFalsos struct {
	mutex   sync.Mutex
	estados []puertos.EstadoCircuito
}

func (eventos *eventosFalsos) PedidoRegistrado(context.Context, string) {}
func (eventos *eventosFalsos) InventarioConsultado(context.Context, string, puertos.ResultadoInventario, error) {
}
func (eventos *eventosFalsos) EventoPublicado(context.Context, string, error) {}
func (eventos *eventosFalsos) CircuitoDelInventarioCambio(_ context.Context, _, nuevo puertos.EstadoCircuito) {
	eventos.mutex.Lock()
	defer eventos.mutex.Unlock()
	eventos.estados = append(eventos.estados, nuevo)
}

var configuracion = inventario.Configuracion{FallosParaAbrir: 5, EsperaAbierto: 50 * time.Millisecond}

func TestSeAbreConCincoFallasYDespuesNoLlama(prueba *testing.T) {
	eventos := &eventosFalsos{}
	disyuntor := inventario.NuevoDisyuntor(inventario.NuevoFalso(inventario.ModoError), eventos, configuracion)

	for intento := range 5 {
		if _, err := disyuntor.Reservar(context.Background(), "pedido", nil); err == nil || errors.Is(err, puertos.ErrInventarioNoDisponible) {
			prueba.Fatalf("intento %d: se esperaba la falla del inventario, dio: %v", intento+1, err)
		}
	}
	inicio := time.Now()
	_, err := disyuntor.Reservar(context.Background(), "pedido", nil)
	if !errors.Is(err, puertos.ErrInventarioNoDisponible) || time.Since(inicio) > 10*time.Millisecond {
		prueba.Errorf("con el circuito abierto se responde al instante sin llamar: %v", err)
	}
	if len(eventos.estados) != 1 || eventos.estados[0] != puertos.CircuitoAbierto {
		prueba.Errorf("se esperaba el aviso de apertura: %v", eventos.estados)
	}
}

func TestUnRechazoNoAbreElCircuito(prueba *testing.T) {
	disyuntor := inventario.NuevoDisyuntor(inventario.NuevoFalso(inventario.ModoRechazo), &eventosFalsos{}, configuracion)

	for range 10 {
		if _, err := disyuntor.Reservar(context.Background(), "pedido", nil); !errors.Is(err, puertos.ErrInventarioRechazo) {
			prueba.Fatalf("se esperaba el rechazo, dio: %v", err)
		}
	}
}

func TestInventarioLentoCortaEnElTimeout(prueba *testing.T) {
	disyuntor := inventario.NuevoDisyuntor(inventario.NuevoFalso(inventario.ModoLento), &eventosFalsos{}, configuracion)

	inicio := time.Now()
	_, err := disyuntor.Reservar(context.Background(), "pedido", nil)

	if err == nil || errors.Is(err, puertos.ErrInventarioRechazo) {
		prueba.Errorf("se esperaba una falla transitoria, dio: %v", err)
	}
	if demora := time.Since(inicio); demora < inventario.TiempoMaximo || demora > inventario.TiempoMaximo+500*time.Millisecond {
		prueba.Errorf("tardó %v, el timeout es de %v", demora, inventario.TiempoMaximo)
	}
}

func TestSinStockEsUnaRespuestaDeNegocio(prueba *testing.T) {
	disyuntor := inventario.NuevoDisyuntor(inventario.NuevoFalso(inventario.ModoSinStock), &eventosFalsos{}, configuracion)

	resultado, err := disyuntor.Reservar(context.Background(), "pedido", nil)

	if err != nil || resultado != puertos.ReservaSinStock {
		prueba.Errorf("resultado %s (%v)", resultado, err)
	}
}
