package inventario_test

import (
	"context"
	"errors"
	"slices"
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
func (eventos *eventosFalsos) ConsumosPendientes(context.Context, int)        {}
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

func (controlado *inventarioControlado) responder() (int64, error) {
	controlado.mutex.Lock()
	defer controlado.mutex.Unlock()
	controlado.llamadas++
	if controlado.err != nil {
		return 0, controlado.err
	}
	return 7, nil
}

func (controlado *inventarioControlado) Reducir(context.Context, int64, int64, string) (int64, error) {
	return controlado.responder()
}

func (controlado *inventarioControlado) Aumentar(context.Context, int64, int64, string) (int64, error) {
	return controlado.responder()
}

func (controlado *inventarioControlado) Consultar(context.Context, int64) (int64, error) {
	return controlado.responder()
}

func TestSeAbreConCincoFallasYDespuesNoLlama(prueba *testing.T) {
	eventos := &eventosFalsos{}
	disyuntor := inventario.NuevoDisyuntor(inventario.NuevoFalso(inventario.ModoError), eventos, configuracion)

	for intento := range 5 {
		if _, err := disyuntor.Reducir(context.Background(), 1, 1, "pedido"); err == nil || errors.Is(err, puertos.ErrInventarioNoDisponible) {
			prueba.Fatalf("intento %d: se esperaba la falla del inventario, dio: %v", intento+1, err)
		}
	}
	inicio := time.Now()
	_, err := disyuntor.Reducir(context.Background(), 1, 1, "pedido")
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
		disyuntor.Reducir(context.Background(), 1, 1, "pedido")
	}

	controlado.fallarCon(nil)
	time.Sleep(configuracion.EsperaAbierto + 20*time.Millisecond)
	stock, err := disyuntor.Reducir(context.Background(), 1, 1, "pedido")

	if err != nil || stock != 7 {
		prueba.Fatalf("la llamada de prueba tenía que pasar: %d (%v)", stock, err)
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
		disyuntor.Reducir(context.Background(), 1, 1, "pedido")
	}

	if controlado.llamadas != 10 || len(eventos.estados) != 0 {
		prueba.Errorf("llegaron %d de 10 llamadas, cambios de estado %v: el circuito tenía que seguir cerrado", controlado.llamadas, eventos.estados)
	}
}

func TestUnRechazoNoAbreElCircuito(prueba *testing.T) {
	disyuntor := inventario.NuevoDisyuntor(inventario.NuevoFalso(inventario.ModoRechazo), &eventosFalsos{}, configuracion)

	for range 10 {
		if _, err := disyuntor.Reducir(context.Background(), 1, 1, "pedido"); !errors.Is(err, puertos.ErrInventarioRechazo) {
			prueba.Fatalf("se esperaba el rechazo, dio: %v", err)
		}
	}
}

func TestInventarioLentoCortaEnElTimeout(prueba *testing.T) {
	disyuntor := inventario.NuevoDisyuntor(inventario.NuevoFalso(inventario.ModoLento), &eventosFalsos{}, configuracion)

	inicio := time.Now()
	_, err := disyuntor.Reducir(context.Background(), 1, 1, "pedido")

	if err == nil || errors.Is(err, puertos.ErrInventarioRechazo) {
		prueba.Errorf("se esperaba una falla transitoria, dio: %v", err)
	}
	if demora := time.Since(inicio); demora < inventario.TiempoMaximo || demora > inventario.TiempoMaximo+500*time.Millisecond {
		prueba.Errorf("tardó %v, el timeout es de %v", demora, inventario.TiempoMaximo)
	}
}

// Un 409 es una respuesta de negocio: el proveedor contestó, no está caído.
func TestStockInsuficienteNoAbreElCircuito(prueba *testing.T) {
	disyuntor := inventario.NuevoDisyuntor(inventario.NuevoFalso(inventario.ModoSinStock), &eventosFalsos{}, configuracion)

	for range 10 {
		if _, err := disyuntor.Reducir(context.Background(), 1, 1, "pedido"); !errors.Is(err, puertos.ErrStockInsuficiente) {
			prueba.Fatalf("se esperaba el 409, dio: %v", err)
		}
	}
}

func TestElFalsoLlevaElStock(prueba *testing.T) {
	falso := inventario.NuevoFalso(inventario.ModoConfirma)
	contexto := context.Background()

	if _, err := falso.Reducir(contexto, 1, 1, "pedido"); !errors.Is(err, puertos.ErrStockInsuficiente) {
		prueba.Errorf("con stock 0 reducir tenía que dar 409, dio: %v", err)
	}
	if stock, err := falso.Aumentar(contexto, 1, 10, "carga"); err != nil || stock != 10 {
		prueba.Errorf("aumentar: %d (%v)", stock, err)
	}
	if stock, err := falso.Reducir(contexto, 1, 4, "pedido"); err != nil || stock != 6 {
		prueba.Errorf("reducir: %d (%v)", stock, err)
	}
	if stock, err := falso.Consultar(contexto, 1); err != nil || stock != 6 {
		prueba.Errorf("consultar: %d (%v)", stock, err)
	}
}
