// Package metricas expone las métricas del servicio para Prometheus (ADR-006):
// las de cada solicitud HTTP y las de los envíos de notificaciones.
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

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

// limitesDeDuracion son los rangos del histograma de duración, en segundos. El
// de 0,3 s está para medir el objetivo de servicio (ADR-006).
var limitesDeDuracion = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.3, 0.5, 1, 3, 5}

// Metricas reúne los contadores del servicio en un registro propio.
type Metricas struct {
	registro    *prometheus.Registry
	solicitudes *prometheus.CounterVec
	duracion    *prometheus.HistogramVec
	envios      *prometheus.CounterVec
}

// Nuevas crea las métricas, además de las del proceso y del runtime de Go
// (memoria, goroutines, CPU).
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
		envios: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "notificaciones_envios_total",
			Help: "Intentos de envío terminados, por resultado: enviada, reintento o fallida.",
		}, []string{"resultado"}),
	}
	metricas.registro.MustRegister(
		metricas.solicitudes, metricas.duracion, metricas.envios,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	// Se crean en cero para que la serie exista desde el arranque: así una alerta
	// sobre las fallidas funciona aunque todavía no haya fallado ninguna.
	for _, resultado := range []puertos.ResultadoEnvio{puertos.EnvioExitoso, puertos.EnvioReintento, puertos.EnvioFallido} {
		metricas.envios.WithLabelValues(string(resultado))
	}
	return metricas
}

// ContarEnvio suma un intento de envío terminado con ese resultado.
func (metricas *Metricas) ContarEnvio(resultado puertos.ResultadoEnvio) {
	metricas.envios.WithLabelValues(string(resultado)).Inc()
}

// Solicitudes es un middleware de Gin que cuenta y mide cada solicitud. Usa la
// ruta con parámetros (/notificaciones/:id) para no crear una serie por id.
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

// Servir expone /metrics en un puerto propio, que el compose no publica: solo
// Prometheus lo lee desde la red interna. Se apaga cuando se cancela el contexto.
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
