package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
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
}

// claveInstancia guarda en el contexto de la solicitud la instancia elegida.
type claveInstancia struct{}

func nuevoProxyBalanceado(servicio string, instancias []*url.URL, tiempoMaximo time.Duration) *proxyBalanceado {
	balanceador := &proxyBalanceado{servicio: servicio, instancias: instancias, tiempoMaximo: tiempoMaximo}
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
	balanceador.proxy.ServeHTTP(escritor, pedido.WithContext(contexto))
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
	escritor.Header().Set("Content-Type", "application/json; charset=utf-8")
	escritor.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(escritor).Encode(map[string]string{
		"codigo":  "no_disponible",
		"mensaje": "Servicio temporalmente no disponible.",
	})
}

// parsearInstancias lee una lista de URLs separadas por comas, como la de
// NOTIFICACIONES_URLS: "http://notificaciones-1:8084,http://notificaciones-2:8084".
func parsearInstancias(texto string) ([]*url.URL, error) {
	var instancias []*url.URL
	for _, parte := range strings.Split(texto, ",") {
		parte = strings.TrimSpace(parte)
		if parte == "" {
			continue
		}
		direccion, err := url.Parse(parte)
		if err != nil || (direccion.Scheme != "http" && direccion.Scheme != "https") || direccion.Host == "" {
			return nil, fmt.Errorf("instancia %q inválida: tiene que ser una URL http://host:puerto", parte)
		}
		instancias = append(instancias, direccion)
	}
	if len(instancias) == 0 {
		return nil, fmt.Errorf("no hay ninguna instancia configurada")
	}
	return instancias, nil
}
