// Package metricas expone las métricas del servicio para Prometheus (ADR-006):
// las de cada solicitud HTTP y las del negocio de productos.
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
)

// limitesDeDuracion son los rangos del histograma de duración, en segundos. El
// de 0,3 s está para medir RNF2 (consultar el catálogo en menos de 300 ms).
var limitesDeDuracion = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.3, 0.5, 1, 2, 5}

// Metricas reúne los contadores del servicio en un registro propio.
type Metricas struct {
	registro    *prometheus.Registry
	solicitudes *prometheus.CounterVec
	duracion    *prometheus.HistogramVec
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
	}
	metricas.registro.MustRegister(
		metricas.solicitudes, metricas.duracion,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return metricas
}

// Solicitudes es un middleware de Gin que cuenta y mide cada solicitud, con la
// ruta con parámetros (/productos/:id) para no crear una serie por id.
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
