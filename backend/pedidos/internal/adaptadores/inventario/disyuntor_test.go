package inventario_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/adaptadores/inventario"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

type eventosFalsos struct {
	mutex   sync.Mutex
	estados []puertos.EstadoCircuito
}

func (eventos *eventosFalsos) PedidoRegistrado(context.Context, string) {}
func (eventos *eventosFalsos) InventarioConsultado(context.Context, string, puertos.ResultadoInventario, error) {
}
func (eventos *eventosFalsos) EventoPublicado(context.Context, string, error)        {}
func (eventos *eventosFalsos) ConfirmacionNoGuardada(context.Context, string, error) {}
func (eventos *eventosFalsos) CircuitoDelInventarioCambio(_ context.Context, _, nuevo puertos.EstadoCircuito) {
	eventos.mutex.Lock()
	defer eventos.mutex.Unlock()
	eventos.estados = append(eventos.estados, nuevo)
}

var configuracion = inventario.Configuracion{FallosParaAbrir: 5, EsperaAbierto: 50 * time.Millisecond}

// inventarioControlado responde con el error que se le indique y cuenta las
// llamadas que le llegan, para simular caídas y recuperaciones.
type inventarioControlado struct {
	mutex    sync.Mutex
	err      error
	llamadas int
}

func (controlado *inventarioControlado) fallarCon(err error) {
	controlado.mutex.Lock()
	defer controlado.mutex.Unlock()
	controlado.err = err
}

func (controlado *inventarioControlado) Reservar(context.Context, string, []dominio.Insumo) (puertos.ResultadoReserva, error) {
	controlado.mutex.Lock()
	defer controlado.mutex.Unlock()
	controlado.llamadas++
	if controlado.err != nil {
		return "", controlado.err
	}
	return puertos.ReservaConfirmada, nil
}

func (controlado *inventarioControlado) Liberar(context.Context, string) error {
	_, err := controlado.Reservar(context.Background(), "", nil)
	return err
}

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

// Pasada la espera, deja pasar una llamada de prueba; si anda, se cierra y el
// inventario vuelve a atender normalmente.
func TestSeCierraSoloCuandoElInventarioVuelve(prueba *testing.T) {
	eventos := &eventosFalsos{}
	controlado := &inventarioControlado{err: errors.New("el inventario respondió 503")}
	disyuntor := inventario.NuevoDisyuntor(controlado, eventos, configuracion)
	for range 5 {
		disyuntor.Reservar(context.Background(), "pedido", nil)
	}

	controlado.fallarCon(nil)
	time.Sleep(configuracion.EsperaAbierto + 20*time.Millisecond)
	resultado, err := disyuntor.Reservar(context.Background(), "pedido", nil)

	if err != nil || resultado != puertos.ReservaConfirmada {
		prueba.Fatalf("la llamada de prueba tenía que pasar: %s (%v)", resultado, err)
	}
	esperados := []puertos.EstadoCircuito{puertos.CircuitoAbierto, puertos.CircuitoSemiabierto, puertos.CircuitoCerrado}
	if !slices.Equal(eventos.estados, esperados) {
		prueba.Errorf("estados %v, se esperaba %v", eventos.estados, esperados)
	}
}

// Una llamada que cortamos nosotros (por ejemplo, al apagar el servicio) no es
// una caída del inventario.
func TestLasCancelacionesNoAbrenElCircuito(prueba *testing.T) {
	eventos := &eventosFalsos{}
	controlado := &inventarioControlado{err: context.Canceled}
	disyuntor := inventario.NuevoDisyuntor(controlado, eventos, configuracion)

	for range 10 {
		disyuntor.Reservar(context.Background(), "pedido", nil)
	}

	if controlado.llamadas != 10 || len(eventos.estados) != 0 {
		prueba.Errorf("llegaron %d de 10 llamadas, cambios de estado %v: el circuito tenía que seguir cerrado", controlado.llamadas, eventos.estados)
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
