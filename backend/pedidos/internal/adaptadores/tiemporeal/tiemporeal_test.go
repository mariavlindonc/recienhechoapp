package tiemporeal

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
)

// medidorFalso cuenta conexiones y mensajes.
type medidorFalso struct {
	abiertas atomic.Int64
	enviados atomic.Int64
}

func (medidor *medidorFalso) ConexionAbierta() { medidor.abiertas.Add(1) }
func (medidor *medidorFalso) ConexionCerrada() { medidor.abiertas.Add(-1) }
func (medidor *medidorFalso) MensajeEnviado()  { medidor.enviados.Add(1) }

var momento = time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC)

func eventoDe(idComprador, idPedido, estado string) dominio.Evento {
	return dominio.Evento{ID: "evento-" + idPedido, OcurridoEn: momento, Datos: dominio.DatosEstadoCambiado{
		IDPedido: idPedido, IDComprador: idComprador, EstadoAnterior: "confirmado", EstadoNuevo: estado,
		EmailComprador: "secreto@ejemplo.com",
	}}
}

// levantar arma un servidor de prueba: el comprador sale de la query, porque en el
// servicio lo pone la API a partir de los headers del gateway.
func levantar(prueba *testing.T) (*Centro, *medidorFalso, string) {
	prueba.Helper()
	medidor := &medidorFalso{}
	centro := Nuevo(medidor, slog.New(slog.DiscardHandler))
	servidor := httptest.NewServer(http.HandlerFunc(func(escritor http.ResponseWriter, pedido *http.Request) {
		centro.Atender(escritor, pedido, pedido.URL.Query().Get("comprador"))
	}))
	prueba.Cleanup(func() {
		centro.Cerrar()
		servidor.Close()
	})
	return centro, medidor, "ws" + strings.TrimPrefix(servidor.URL, "http")
}

func conectar(prueba *testing.T, direccion, comprador string) *websocket.Conn {
	prueba.Helper()
	contexto, cancelar := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelar()
	socket, _, err := websocket.Dial(contexto, direccion+"?comprador="+comprador, nil)
	if err != nil {
		prueba.Fatalf("conectar: %v", err)
	}
	prueba.Cleanup(func() { socket.CloseNow() })
	return socket
}

// esperarQue repite la condición hasta que se cumple: lo que pasa del otro lado de
// la conexión no es instantáneo.
func esperarQue(prueba *testing.T, descripcion string, condicion func() bool) {
	prueba.Helper()
	for limite := time.Now().Add(2 * time.Second); time.Now().Before(limite); time.Sleep(5 * time.Millisecond) {
		if condicion() {
			return
		}
	}
	prueba.Fatalf("no se cumplió: %s", descripcion)
}

func leerMensaje(prueba *testing.T, socket *websocket.Conn) map[string]any {
	prueba.Helper()
	contexto, cancelar := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelar()
	_, contenido, err := socket.Read(contexto)
	if err != nil {
		prueba.Fatalf("leer: %v", err)
	}
	var mensaje map[string]any
	if err := json.Unmarshal(contenido, &mensaje); err != nil {
		prueba.Fatalf("el mensaje no es JSON: %s", contenido)
	}
	return mensaje
}

func TestElCompradorRecibeElCambioDeSuPedido(prueba *testing.T) {
	centro, medidor, direccion := levantar(prueba)
	socket := conectar(prueba, direccion, "ana")
	esperarQue(prueba, "la conexión queda registrada", func() bool { return medidor.abiertas.Load() == 1 })

	centro.Avisar(eventoDe("ana", "pedido-1", "listo"))

	mensaje := leerMensaje(prueba, socket)
	esperado := map[string]any{
		"tipo": "estado_cambiado", "idPedido": "pedido-1", "estadoAnterior": "confirmado", "estadoNuevo": "listo",
		"ocurridoEn": "2026-11-06T15:00:00Z",
	}
	if len(mensaje) != len(esperado) {
		prueba.Errorf("mensaje = %v: solo van los campos del ADR-018 (nada de email)", mensaje)
	}
	for campo, valor := range esperado {
		if mensaje[campo] != valor {
			prueba.Errorf("%s = %v, se esperaba %v", campo, mensaje[campo], valor)
		}
	}
	esperarQue(prueba, "se cuenta el mensaje enviado", func() bool { return medidor.enviados.Load() == 1 })
}

// B2: un comprador recibe solo los cambios de sus pedidos.
func TestUnCompradorNoRecibeLosCambiosDeOtro(prueba *testing.T) {
	centro, medidor, direccion := levantar(prueba)
	ana := conectar(prueba, direccion, "ana")
	beto := conectar(prueba, direccion, "beto")
	esperarQue(prueba, "los dos conectados", func() bool { return medidor.abiertas.Load() == 2 })

	centro.Avisar(eventoDe("beto", "de-beto", "listo"))
	centro.Avisar(eventoDe("ana", "de-ana", "listo"))

	if mensaje := leerMensaje(prueba, ana); mensaje["idPedido"] != "de-ana" {
		prueba.Errorf("ana recibió %v", mensaje)
	}
	if mensaje := leerMensaje(prueba, beto); mensaje["idPedido"] != "de-beto" {
		prueba.Errorf("beto recibió %v", mensaje)
	}
}

// Un comprador con dos pestañas recibe el aviso en las dos.
func TestLasDosPestaniasRecibenElAviso(prueba *testing.T) {
	centro, medidor, direccion := levantar(prueba)
	primera := conectar(prueba, direccion, "ana")
	segunda := conectar(prueba, direccion, "ana")
	esperarQue(prueba, "las dos conectadas", func() bool { return medidor.abiertas.Load() == 2 })

	centro.Avisar(eventoDe("ana", "pedido-1", "listo"))

	leerMensaje(prueba, primera)
	leerMensaje(prueba, segunda)
}

func TestAlDesconectarseDejaDeContar(prueba *testing.T) {
	centro, medidor, direccion := levantar(prueba)
	socket := conectar(prueba, direccion, "ana")
	esperarQue(prueba, "conectado", func() bool { return medidor.abiertas.Load() == 1 })

	socket.Close(websocket.StatusNormalClosure, "")

	esperarQue(prueba, "la conexión deja de contar", func() bool { return medidor.abiertas.Load() == 0 })
	centro.mutex.Lock()
	defer centro.mutex.Unlock()
	if len(centro.conexiones) != 0 {
		prueba.Errorf("el comprador sigue registrado: %v", centro.conexiones)
	}
}

// Al apagar el servicio, http.Server.Shutdown no espera a los WebSocket: los corta
// el centro, con un código que le dice al navegador que vuelva a conectarse.
func TestCerrarCortaLasConexiones(prueba *testing.T) {
	centro, medidor, direccion := levantar(prueba)
	socket := conectar(prueba, direccion, "ana")
	esperarQue(prueba, "conectado", func() bool { return medidor.abiertas.Load() == 1 })

	// Cerrar espera a que el navegador conteste el cierre, así que corre aparte.
	cerrado := make(chan struct{})
	go func() {
		defer close(cerrado)
		centro.Cerrar()
	}()

	contexto, cancelar := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelar()
	_, _, err := socket.Read(contexto)
	var cierre websocket.CloseError
	if !errors.As(err, &cierre) || cierre.Code != websocket.StatusGoingAway {
		prueba.Errorf("se esperaba el cierre StatusGoingAway, dio: %v", err)
	}
	<-cerrado
	if medidor.abiertas.Load() != 0 {
		prueba.Errorf("quedaron %d conexiones abiertas", medidor.abiertas.Load())
	}
}

// Avisar no puede frenar a quien guarda un pedido: si un navegador no lee y se le
// llena la cola, se le corta la conexión en vez de esperarlo.
func TestUnCompradorLentoNoFrenaAvisar(prueba *testing.T) {
	centro := Nuevo(&medidorFalso{}, slog.New(slog.DiscardHandler))
	lento := centro.sumar("ana")

	termino := make(chan struct{})
	go func() {
		defer close(termino)
		for range largoDeCola + 5 {
			centro.Avisar(eventoDe("ana", "pedido-1", "listo"))
		}
	}()

	select {
	case <-termino:
	case <-time.After(2 * time.Second):
		prueba.Fatal("Avisar se quedó esperando a un comprador que no lee")
	}
	select {
	case <-lento.demorada:
	default:
		prueba.Error("a un comprador con la cola llena se le tiene que cortar la conexión")
	}
}

// Sin nadie conectado, Avisar no hace nada: es lo normal.
func TestAvisarSinNadieConectado(prueba *testing.T) {
	centro := Nuevo(&medidorFalso{}, slog.New(slog.DiscardHandler))

	centro.Avisar(eventoDe("ana", "pedido-1", "listo"))
}
