package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// proxyBalanceado reenvía las solicitudes a las instancias de un servicio,
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
}

// claveInstancia guarda en el contexto de la solicitud la instancia elegida.
type claveInstancia struct{}

func nuevoProxyBalanceado(servicio string, instancias []*url.URL, tiempoMaximo time.Duration, alTerminar func(instancia string, estado int)) *proxyBalanceado {
	balanceador := &proxyBalanceado{servicio: servicio, instancias: instancias, tiempoMaximo: tiempoMaximo, alTerminar: alTerminar}
	balanceador.proxy = &httputil.ReverseProxy{
		Rewrite: func(pedido *httputil.ProxyRequest) {
			elegida := pedido.In.Context().Value(claveInstancia{}).(*url.URL)
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
	elegida := balanceador.elegir()
	contexto, cancelar := context.WithTimeout(pedido.Context(), balanceador.tiempoMaximo)
	defer cancelar()
	contexto = context.WithValue(contexto, claveInstancia{}, elegida)

	conEstado := &escritorConEstado{ResponseWriter: escritor, estado: http.StatusOK}
	balanceador.proxy.ServeHTTP(conEstado, pedido.WithContext(contexto))
	if balanceador.alTerminar != nil {
		balanceador.alTerminar(elegida.Host, conEstado.estado)
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

// elegir devuelve la próxima instancia del turno.
func (balanceador *proxyBalanceado) elegir() *url.URL {
	turno := balanceador.siguiente.Add(1) - 1
	return balanceador.instancias[turno%uint64(len(balanceador.instancias))]
}

// responderFalla se llama cuando la instancia no contestó (caída, timeout): el
// consumidor recibe el error del contrato y el detalle queda en el log.
func (balanceador *proxyBalanceado) responderFalla(escritor http.ResponseWriter, pedido *http.Request, err error) {
	slog.ErrorContext(pedido.Context(), "el servicio no respondió",
		"servicio", balanceador.servicio,
		"instancia", pedido.Context().Value(claveInstancia{}).(*url.URL).Host,
		"metodo", pedido.Method, "ruta", pedido.URL.Path, "error", err)
	responderNoDisponible(escritor)
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
