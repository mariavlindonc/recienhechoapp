// Package tiemporeal mantiene los WebSocket de los compradores que siguen sus
// pedidos en vivo y cumple el puerto AvisosEnVivo (ADR-018). El WebSocket solo avisa
// que algo cambió: la pantalla pide el estado por HTTP al conectarse y al reconectarse.
package tiemporeal

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

const (
	// largoDeCola es cuántos mensajes puede tener pendientes una conexión. Un estado
	// cambia pocas veces: si se llena, el navegador no está leyendo.
	largoDeCola = 16
	// intervaloDePing es cada cuánto se comprueba que la conexión sigue viva (ADR-018).
	intervaloDePing = 30 * time.Second
	// esperaPorEscritura es lo máximo que se espera a un navegador en cada envío o ping.
	esperaPorEscritura = 5 * time.Second
)

// tipoEstadoCambiado es el tipo de mensaje de un cambio de estado. El navegador
// ignora los que no conoce, así se pueden sumar otros, como la ubicación del delivery.
const tipoEstadoCambiado = "estado_cambiado"

// medidor es lo que este adaptador necesita de las métricas. Lo cumple
// *metricas.Metricas; en los tests se usa uno falso.
type medidor interface {
	ConexionAbierta()
	ConexionCerrada()
	MensajeEnviado()
}

// Centro sabe qué compradores están conectados y les reparte los avisos.
type Centro struct {
	metricas medidor
	logger   *slog.Logger

	mutex      sync.Mutex
	conexiones map[string]map[*conexion]struct{} // por id de comprador
	cerrado    bool

	apagando chan struct{}
	activas  sync.WaitGroup
}

var _ puertos.AvisosEnVivo = (*Centro)(nil)

// conexion es una pestaña de un comprador.
type conexion struct {
	salida chan []byte
	// demorada se cierra si el navegador no lee y se le llena la cola.
	demorada chan struct{}
}

// mensaje es lo que recibe el navegador (ADR-018). Lleva solo lo que la pantalla
// necesita: nada de los datos personales que trae el evento.
type mensaje struct {
	Tipo           string    `json:"tipo"`
	IDPedido       string    `json:"idPedido"`
	EstadoAnterior string    `json:"estadoAnterior,omitempty"`
	EstadoNuevo    string    `json:"estadoNuevo"`
	OcurridoEn     time.Time `json:"ocurridoEn"`
}

// Nuevo arma el centro, sin conexiones.
func Nuevo(metricas medidor, logger *slog.Logger) *Centro {
	return &Centro{metricas: metricas, logger: logger, conexiones: map[string]map[*conexion]struct{}{}, apagando: make(chan struct{})}
}

// Avisar le deja el cambio en la cola a cada conexión del dueño del pedido, sin
// esperar a nadie: lo llama la aplicación justo después de guardar.
func (centro *Centro) Avisar(evento dominio.Evento) {
	centro.mutex.Lock()
	defer centro.mutex.Unlock()
	destinos := centro.conexiones[evento.Datos.IDComprador]
	if len(destinos) == 0 {
		return
	}
	contenido, err := json.Marshal(mensaje{
		Tipo:           tipoEstadoCambiado,
		IDPedido:       evento.Datos.IDPedido,
		EstadoAnterior: evento.Datos.EstadoAnterior,
		EstadoNuevo:    evento.Datos.EstadoNuevo,
		OcurridoEn:     evento.OcurridoEn,
	})
	if err != nil {
		centro.logger.Error("no se pudo armar el aviso en vivo", "id_pedido", evento.Datos.IDPedido, "error", err.Error())
		return
	}
	for destino := range destinos {
		select {
		case destino.salida <- contenido:
		default:
			destino.marcarDemorada()
		}
	}
}

func (destino *conexion) marcarDemorada() {
	select {
	case <-destino.demorada:
	default:
		close(destino.demorada)
	}
}

// Atender convierte la solicitud en un WebSocket y lo mantiene hasta que se corta.
// El comprador ya viene identificado por la API.
func (centro *Centro) Atender(escritor http.ResponseWriter, pedido *http.Request, idComprador string) {
	socket, err := websocket.Accept(escritor, pedido, nil)
	if err != nil {
		centro.logger.WarnContext(pedido.Context(), "no se pudo abrir el WebSocket", "id_comprador", idComprador, "error", err.Error())
		return
	}
	defer socket.CloseNow()

	actual := centro.sumar(idComprador)
	if actual == nil {
		socket.Close(websocket.StatusGoingAway, "el servicio se está apagando")
		return
	}
	defer centro.sacar(idComprador, actual)
	centro.metricas.ConexionAbierta()
	defer centro.metricas.ConexionCerrada()
	centro.logger.InfoContext(pedido.Context(), "comprador conectado en vivo", "id_comprador", idComprador)
	defer centro.logger.InfoContext(pedido.Context(), "comprador desconectado", "id_comprador", idComprador)

	// El navegador no manda nada: CloseRead lee por nosotros para atender los pongs y
	// el cierre, y cancela el contexto cuando la conexión se corta.
	contexto := socket.CloseRead(pedido.Context())
	centro.escribir(contexto, socket, actual)
}

// escribir manda los mensajes de la cola y los pings hasta que la conexión termina.
func (centro *Centro) escribir(contexto context.Context, socket *websocket.Conn, actual *conexion) {
	reloj := time.NewTicker(intervaloDePing)
	defer reloj.Stop()
	for {
		select {
		case <-contexto.Done():
			return
		case <-centro.apagando:
			socket.Close(websocket.StatusGoingAway, "el servicio se está apagando")
			return
		case <-actual.demorada:
			socket.Close(websocket.StatusTryAgainLater, "la conexión no da abasto")
			return
		case contenido := <-actual.salida:
			contextoEscritura, cancelar := context.WithTimeout(contexto, esperaPorEscritura)
			err := socket.Write(contextoEscritura, websocket.MessageText, contenido)
			cancelar()
			if err != nil {
				return
			}
			centro.metricas.MensajeEnviado()
		case <-reloj.C:
			contextoPing, cancelar := context.WithTimeout(contexto, esperaPorEscritura)
			err := socket.Ping(contextoPing)
			cancelar()
			if err != nil {
				return
			}
		}
	}
}

// sumar registra una conexión. Devuelve nil si el servicio se está apagando.
func (centro *Centro) sumar(idComprador string) *conexion {
	centro.mutex.Lock()
	defer centro.mutex.Unlock()
	if centro.cerrado {
		return nil
	}
	nueva := &conexion{salida: make(chan []byte, largoDeCola), demorada: make(chan struct{})}
	if centro.conexiones[idComprador] == nil {
		centro.conexiones[idComprador] = map[*conexion]struct{}{}
	}
	centro.conexiones[idComprador][nueva] = struct{}{}
	centro.activas.Add(1)
	return nueva
}

func (centro *Centro) sacar(idComprador string, actual *conexion) {
	defer centro.activas.Done()
	centro.mutex.Lock()
	defer centro.mutex.Unlock()
	delete(centro.conexiones[idComprador], actual)
	if len(centro.conexiones[idComprador]) == 0 {
		delete(centro.conexiones, idComprador)
	}
}

// Cerrar corta todas las conexiones y espera a que terminen. http.Server.Shutdown no
// lo hace por nosotros: un WebSocket ya no es una solicitud HTTP en curso.
func (centro *Centro) Cerrar() {
	centro.mutex.Lock()
	if centro.cerrado {
		centro.mutex.Unlock()
		return
	}
	centro.cerrado = true
	close(centro.apagando)
	centro.mutex.Unlock()
	centro.activas.Wait()
}
