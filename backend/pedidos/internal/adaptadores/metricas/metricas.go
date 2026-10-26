// Package metricas expone las métricas del servicio para Prometheus (ADR-006):
// las de cada solicitud HTTP y las del negocio de pedidos.
package metricas

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

// limitesDeDuracion son los rangos del histograma de duración, en segundos. El
// de 2 s está para medir RNF3 (crear un pedido en menos de 2 s).
var limitesDeDuracion = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.3, 0.5, 1, 2, 3, 5}

var estadosDelCircuito = []puertos.EstadoCircuito{puertos.CircuitoCerrado, puertos.CircuitoSemiabierto, puertos.CircuitoAbierto}

// Metricas reúne los contadores del servicio en un registro propio.
type Metricas struct {
	registro    *prometheus.Registry
	solicitudes *prometheus.CounterVec
	duracion    *prometheus.HistogramVec
	registrados *prometheus.CounterVec
	inventario  *prometheus.CounterVec
	circuito    *prometheus.GaugeVec
	publicados  *prometheus.CounterVec
}

// Nuevas crea las métricas, además de las del proceso y del runtime de Go.
func Nuevas() *Metricas {
	metricas := &Metricas{
		registro: prometheus.NewRegistry(),
		solicitudes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "solicitudes_http_total",
			Help: "Solicitudes HTTP atendidas, por método, ruta y estado.",
		}, []string{"metodo", "ruta", "estado"}),
		duracion: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "duracion_solicitud_http_segundos",
			Help:    "Duración de las solicitudes HTTP, por método y ruta.",
			Buckets: limitesDeDuracion,
		}, []string{"metodo", "ruta"}),
		registrados: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pedidos_registrados_total",
			Help: "Pedidos que se intentaron registrar, por resultado: aceptado o sin_cupo.",
		}, []string{"resultado"}),
		inventario: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pedidos_inventario_llamadas_total",
			Help: "Llamadas al inventario del proveedor, por resultado.",
		}, []string{"resultado"}),
		circuito: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pedidos_circuito_inventario",
			Help: "Estado del disyuntor del inventario: vale 1 en el estado actual y 0 en los demás.",
		}, []string{"estado"}),
		publicados: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pedidos_eventos_publicados_total",
			Help: "Eventos de la bandeja de salida que se intentaron publicar, por resultado: publicado o error.",
		}, []string{"resultado"}),
	}
	metricas.registro.MustRegister(
		metricas.solicitudes, metricas.duracion, metricas.registrados, metricas.inventario, metricas.circuito, metricas.publicados,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	// Las series existen desde el arranque, así las alertas funcionan aunque todavía no haya pasado nada.
	for _, resultado := range []string{"aceptado", "sin_cupo"} {
		metricas.registrados.WithLabelValues(resultado)
	}
	for _, resultado := range []puertos.ResultadoInventario{
		puertos.InventarioConfirmada, puertos.InventarioSinStock, puertos.InventarioLiberada,
		puertos.InventarioRechazada, puertos.InventarioFallaTransit, puertos.InventarioNoDisponible,
	} {
		metricas.inventario.WithLabelValues(string(resultado))
	}
	for _, resultado := range []string{"publicado", "error"} {
		metricas.publicados.WithLabelValues(resultado)
	}
	metricas.MarcarCircuito(puertos.CircuitoCerrado)
	return metricas
}

// ContarRegistro suma un pedido registrado o rechazado por cupo.
func (metricas *Metricas) ContarRegistro(resultado string) {
	metricas.registrados.WithLabelValues(resultado).Inc()
}

// ContarInventario suma una llamada al inventario.
func (metricas *Metricas) ContarInventario(resultado puertos.ResultadoInventario) {
	metricas.inventario.WithLabelValues(string(resultado)).Inc()
}

// MarcarCircuito deja en 1 el estado actual del disyuntor.
func (metricas *Metricas) MarcarCircuito(actual puertos.EstadoCircuito) {
	for _, estado := range estadosDelCircuito {
		valor := 0.0
		if estado == actual {
			valor = 1
		}
		metricas.circuito.WithLabelValues(string(estado)).Set(valor)
	}
}

// ContarPublicacion suma un intento de publicar un evento.
func (metricas *Metricas) ContarPublicacion(resultado string) {
	metricas.publicados.WithLabelValues(resultado).Inc()
}

// Solicitudes es un middleware de Gin que cuenta y mide cada solicitud, con la
// ruta con parámetros (/pedidos/:id) para no crear una serie por id.
func (metricas *Metricas) Solicitudes() gin.HandlerFunc {
	return func(contexto *gin.Context) {
		inicio := time.Now()
		contexto.Next()

		ruta := contexto.FullPath()
		if ruta == "" {
			ruta = "sin_ruta"
		}
		metodo := contexto.Request.Method
		metricas.solicitudes.WithLabelValues(metodo, ruta, strconv.Itoa(contexto.Writer.Status())).Inc()
		metricas.duracion.WithLabelValues(metodo, ruta).Observe(time.Since(inicio).Seconds())
	}
}

// Servir expone /metrics en un puerto propio que el compose no publica. Se apaga
// cuando se cancela el contexto.
func (metricas *Metricas) Servir(contexto context.Context, puerto string) error {
	enrutador := http.NewServeMux()
	enrutador.Handle("/metrics", promhttp.HandlerFor(metricas.registro, promhttp.HandlerOpts{}))
	servidor := &http.Server{Addr: ":" + puerto, Handler: enrutador, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-contexto.Done()
		servidor.Shutdown(context.Background())
	}()
	slog.Info("métricas escuchando", "puerto", puerto)
	if err := servidor.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("servidor de métricas: %w", err)
	}
	return nil
}
