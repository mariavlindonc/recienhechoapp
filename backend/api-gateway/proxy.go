package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// fallasParaSacar es cuántos chequeos fallidos seguidos sacan a una instancia del
// balanceo (ADR-012).
const fallasParaSacar = 2

// proxyBalanceado reenvía las solicitudes a las instancias sanas de un servicio,
// turnándolas en round-robin (ADR-012). Si la instancia no contesta dentro del
// tiempo máximo, responde 503 no_disponible y no reintenta: el consumidor
// reintenta con la misma Idempotency-Key (ADR-003).
type proxyBalanceado struct {
	servicio     string
	instancias   []*url.URL
	siguiente    atomic.Uint64
	tiempoMaximo time.Duration
	proxy        *httputil.ReverseProxy
	// alTerminar se llama después de cada reenvío con la instancia y el estado de
	// la respuesta (para las métricas). Puede ser nil.
	alTerminar func(instancia string, estado int)
	// alCambiarSalud se llama cuando una instancia sale del balanceo o vuelve (para
	// las métricas). Puede ser nil.
	alCambiarSalud func(instancia string, sana bool)

	mutex sync.Mutex
	// sanas y fallasSeguidas van en el mismo orden que instancias.
	sanas          []bool
	fallasSeguidas []int
}

// claveInstancia guarda en el contexto de la solicitud la instancia elegida.
type claveInstancia struct{}

// Todas las instancias arrancan sanas: el primer chequeo corrige las que no lo están.
func nuevoProxyBalanceado(servicio string, instancias []*url.URL, tiempoMaximo time.Duration, alTerminar func(instancia string, estado int)) *proxyBalanceado {
	balanceador := &proxyBalanceado{
		servicio: servicio, instancias: instancias, tiempoMaximo: tiempoMaximo, alTerminar: alTerminar,
		sanas: make([]bool, len(instancias)), fallasSeguidas: make([]int, len(instancias)),
	}
	for indice := range balanceador.sanas {
		balanceador.sanas[indice] = true
	}
	balanceador.proxy = &httputil.ReverseProxy{
		Rewrite: func(pedido *httputil.ProxyRequest) {
			elegida := balanceador.instancias[pedido.In.Context().Value(claveInstancia{}).(int)]
			pedido.SetURL(elegida)
			pedido.SetXForwarded()
		},
		ErrorHandler: balanceador.responderFalla,
		// otelhttp abre un span por cada reenvío y agrega el header traceparent:
		// así notificaciones continúa la misma traza (traza distribuida).
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	return balanceador
}

func (balanceador *proxyBalanceado) ServeHTTP(escritor http.ResponseWriter, pedido *http.Request) {
	elegida, hay := balanceador.elegir()
	if !hay {
		slog.WarnContext(pedido.Context(), "no hay instancias sanas", "destino", balanceador.servicio)
		responderNoDisponible(escritor)
		return
	}
	contexto, cancelar := context.WithTimeout(pedido.Context(), balanceador.tiempoMaximo)
	defer cancelar()
	contexto = context.WithValue(contexto, claveInstancia{}, elegida)

	conEstado := &escritorConEstado{ResponseWriter: escritor, estado: http.StatusOK}
	balanceador.proxy.ServeHTTP(conEstado, pedido.WithContext(contexto))
	if balanceador.alTerminar != nil {
		balanceador.alTerminar(balanceador.instancias[elegida].Host, conEstado.estado)
	}
}

// escritorConEstado recuerda el código de estado de la respuesta para poder
// contarlo después.
type escritorConEstado struct {
	http.ResponseWriter
	estado int
}

func (escritor *escritorConEstado) WriteHeader(estado int) {
	escritor.estado = estado
	escritor.ResponseWriter.WriteHeader(estado)
}

// Unwrap deja que el proxy llegue al escritor original (por ejemplo, para Flush).
func (escritor *escritorConEstado) Unwrap() http.ResponseWriter {
	return escritor.ResponseWriter
}

// elegir devuelve el índice de la próxima instancia sana del turno, o false si no
// hay ninguna.
func (balanceador *proxyBalanceado) elegir() (int, bool) {
	turno := balanceador.siguiente.Add(1) - 1
	cantidad := uint64(len(balanceador.instancias))
	balanceador.mutex.Lock()
	defer balanceador.mutex.Unlock()
	for salto := range cantidad {
		indice := int((turno + salto) % cantidad)
		if balanceador.sanas[indice] {
			return indice, true
		}
	}
	return 0, false
}

// responderFalla se llama cuando la instancia no contestó (caída, timeout): el
// consumidor recibe el error del contrato y el detalle queda en el log. La
// instancia sale del balanceo sin esperar al próximo chequeo (chequeo pasivo),
// salvo que el que cortó haya sido el cliente.
func (balanceador *proxyBalanceado) responderFalla(escritor http.ResponseWriter, pedido *http.Request, err error) {
	indice := pedido.Context().Value(claveInstancia{}).(int)
	// "destino" y no "servicio": servicio ya es api-gateway en todas las líneas (ADR-006).
	slog.ErrorContext(pedido.Context(), "el servicio no respondió",
		"destino", balanceador.servicio,
		"instancia", balanceador.instancias[indice].Host,
		"metodo", pedido.Method, "ruta", pedido.URL.Path, "error", err)
	if !errors.Is(err, context.Canceled) {
		balanceador.sacarPorFalla(indice, err)
	}
	responderNoDisponible(escritor)
}

// sacarPorFalla saca del balanceo a una instancia que falló al reenviarle una
// solicitud. Con una sola instancia no se saca: no hay otra a la cual mandar el
// tráfico, y sin chequeo activo no volvería.
func (balanceador *proxyBalanceado) sacarPorFalla(indice int, err error) {
	if len(balanceador.instancias) < 2 {
		return
	}
	balanceador.mutex.Lock()
	defer balanceador.mutex.Unlock()
	balanceador.fallasSeguidas[indice] = fallasParaSacar
	balanceador.cambiarSalud(indice, false, "falló un reenvío: "+err.Error())
}

// registrarChequeo anota el resultado de un chequeo de /vivo: una respuesta OK la
// vuelve a sumar y dos fallas seguidas la sacan.
func (balanceador *proxyBalanceado) registrarChequeo(indice int, err error) {
	balanceador.mutex.Lock()
	defer balanceador.mutex.Unlock()
	if err == nil {
		balanceador.fallasSeguidas[indice] = 0
		balanceador.cambiarSalud(indice, true, "respondió el chequeo")
		return
	}
	balanceador.fallasSeguidas[indice]++
	if balanceador.fallasSeguidas[indice] >= fallasParaSacar {
		balanceador.cambiarSalud(indice, false, "falló el chequeo: "+err.Error())
	}
}

// cambiarSalud deja el estado de la instancia y avisa solo si cambió. Se llama
// con el mutex tomado.
func (balanceador *proxyBalanceado) cambiarSalud(indice int, sana bool, motivo string) {
	if balanceador.sanas[indice] == sana {
		return
	}
	balanceador.sanas[indice] = sana
	instancia := balanceador.instancias[indice].Host
	if sana {
		slog.Info("instancia de vuelta en el balanceo", "destino", balanceador.servicio, "instancia", instancia, "motivo", motivo)
	} else {
		slog.Warn("instancia fuera del balanceo", "destino", balanceador.servicio, "instancia", instancia, "motivo", motivo)
	}
	if balanceador.alCambiarSalud != nil {
		balanceador.alCambiarSalud(instancia, sana)
	}
}

// vigilar consulta /vivo de cada instancia cada intervalo (chequeo activo) hasta
// que se cancela el contexto. Usa /vivo y no /health porque /health hace ping a
// MongoDB, que comparten las instancias: una caída de Mongo las sacaría a todas.
func (balanceador *proxyBalanceado) vigilar(contexto context.Context, intervalo, tiempoMaximo time.Duration) {
	cliente := &http.Client{Timeout: tiempoMaximo}
	if balanceador.alCambiarSalud != nil {
		for _, instancia := range balanceador.instancias {
			balanceador.alCambiarSalud(instancia.Host, true)
		}
	}
	reloj := time.NewTicker(intervalo)
	defer reloj.Stop()
	for {
		select {
		case <-contexto.Done():
			return
		case <-reloj.C:
		}
		for indice, instancia := range balanceador.instancias {
			balanceador.registrarChequeo(indice, chequearVivo(contexto, cliente, instancia))
		}
	}
}

// chequearVivo pide /vivo a la instancia y devuelve error si no responde 200.
func chequearVivo(contexto context.Context, cliente *http.Client, instancia *url.URL) error {
	pedido, err := http.NewRequestWithContext(contexto, http.MethodGet, instancia.JoinPath("/vivo").String(), nil)
	if err != nil {
		return fmt.Errorf("armar el chequeo: %w", err)
	}
	respuesta, err := cliente.Do(pedido)
	if err != nil {
		return err
	}
	respuesta.Body.Close()
	if respuesta.StatusCode != http.StatusOK {
		return fmt.Errorf("/vivo respondió %d", respuesta.StatusCode)
	}
	return nil
}

func responderNoDisponible(escritor http.ResponseWriter) {
	responderError(escritor, http.StatusServiceUnavailable, "no_disponible", "Servicio temporalmente no disponible.")
}

// responderError escribe el error con la forma del contrato: {codigo, mensaje}.
func responderError(escritor http.ResponseWriter, estado int, codigo, mensaje string) {
	escritor.Header().Set("Content-Type", "application/json; charset=utf-8")
	escritor.WriteHeader(estado)
	json.NewEncoder(escritor).Encode(map[string]string{"codigo": codigo, "mensaje": mensaje})
}
