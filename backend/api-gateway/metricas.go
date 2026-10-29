package main

// Métricas del gateway para Prometheus (ADR-006 y ADR-012). La parte HTTP es una
// copia de la de notificaciones: un servicio no importa código de otro.

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
)

// limitesDeDuracion son los rangos del histograma de duración, en segundos. El
// de 0,3 s está para medir el objetivo de servicio (ADR-006).
var limitesDeDuracion = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.3, 0.5, 1, 3, 5}

type metricasGateway struct {
	registro    *prometheus.Registry
	solicitudes *prometheus.CounterVec
	duracion    *prometheus.HistogramVec
	reenvios    *prometheus.CounterVec
	sanas       *prometheus.GaugeVec
}

func nuevasMetricas() *metricasGateway {
	metricas := &metricasGateway{
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
		reenvios: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_solicitudes_total",
			Help: "Solicitudes reenviadas a cada instancia de cada servicio, por estado de la respuesta (ADR-012).",
		}, []string{"servicio", "instancia", "estado"}),
		sanas: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gateway_instancia_sana",
			Help: "1 si la instancia está en el balanceo y 0 si quedó afuera (ADR-012).",
		}, []string{"servicio", "instancia"}),
	}
	metricas.registro.MustRegister(
		metricas.solicitudes, metricas.duracion, metricas.reenvios, metricas.sanas,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return metricas
}

// medirSolicitudes es un middleware de Gin que cuenta y mide cada solicitud.
func (metricas *metricasGateway) medirSolicitudes() gin.HandlerFunc {
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

// contarReenvio devuelve la función que el proxy llama al terminar cada reenvío.
func (metricas *metricasGateway) contarReenvio(servicio string) func(instancia string, estado int) {
	return func(instancia string, estado int) {
		metricas.reenvios.WithLabelValues(servicio, instancia, strconv.Itoa(estado)).Inc()
	}
}

// marcarSalud devuelve la función que el proxy llama cuando una instancia sale del
// balanceo o vuelve.
func (metricas *metricasGateway) marcarSalud(servicio string) func(instancia string, sana bool) {
	return func(instancia string, sana bool) {
		valor := 0.0
		if sana {
			valor = 1
		}
		metricas.sanas.WithLabelValues(servicio, instancia).Set(valor)
	}
}

// servir expone /metrics en un puerto propio, que el compose no publica: solo
// Prometheus lo lee desde la red interna. Se apaga cuando se cancela el contexto.
func (metricas *metricasGateway) servir(contexto context.Context, puerto string) error {
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
