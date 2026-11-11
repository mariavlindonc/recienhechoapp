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
	registro         *prometheus.Registry
	solicitudes      *prometheus.CounterVec
	duracion         *prometheus.HistogramVec
	aciertos         *prometheus.CounterVec
	fallos           *prometheus.CounterVec
	erroresDeCache   *prometheus.CounterVec
	indexados        *prometheus.CounterVec
	antiguedadIndice prometheus.Gauge
}

var tiposDeCache = []string{"producto", "catalogo", "categorias"}

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
		aciertos: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cache_aciertos_total",
			Help: "Lecturas que encontraron el dato en la caché, por tipo: producto, catalogo o categorias (ADR-009).",
		}, []string{"tipo"}),
		fallos: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cache_fallos_total",
			Help: "Lecturas que no encontraron el dato en la caché y fueron a MySQL, por tipo.",
		}, []string{"tipo"}),
		erroresDeCache: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cache_errores_total",
			Help: "Operaciones con Redis que fallaron o tardaron más de 50 ms; se siguió sin caché.",
		}, []string{"operacion"}),
		indexados: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "indice_sincronizaciones_total",
			Help: "Vueltas del proceso que refleja los cambios en la búsqueda, por resultado: ok o error (ADR-008).",
		}, []string{"resultado"}),
		antiguedadIndice: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "indice_pendiente_antiguedad_segundos",
			Help: "Hace cuánto espera el cambio más viejo sin reflejarse en la búsqueda (RNF1: menos de 10 s).",
		}),
	}
	metricas.registro.MustRegister(
		metricas.solicitudes, metricas.duracion, metricas.aciertos, metricas.fallos, metricas.erroresDeCache,
		metricas.indexados, metricas.antiguedadIndice,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	// Las series existen desde el arranque, así la tasa de aciertos se puede calcular aunque todavía no haya lecturas.
	for _, tipo := range tiposDeCache {
		metricas.aciertos.WithLabelValues(tipo)
		metricas.fallos.WithLabelValues(tipo)
	}
	for _, resultado := range []string{"ok", "error"} {
		metricas.indexados.WithLabelValues(resultado)
	}
	return metricas
}

// ContarLecturaDeCache suma un acierto o un fallo de la caché.
func (metricas *Metricas) ContarLecturaDeCache(tipo string, acierto bool) {
	if acierto {
		metricas.aciertos.WithLabelValues(tipo).Inc()
		return
	}
	metricas.fallos.WithLabelValues(tipo).Inc()
}

// ContarErrorDeCache suma una operación con Redis que falló.
func (metricas *Metricas) ContarErrorDeCache(operacion string) {
	metricas.erroresDeCache.WithLabelValues(operacion).Inc()
}

// ContarSincronizacion suma una vuelta del proceso del índice.
func (metricas *Metricas) ContarSincronizacion(resultado string) {
	metricas.indexados.WithLabelValues(resultado).Inc()
}

// MarcarAntiguedadDelIndice deja la antigüedad del cambio más viejo sin indexar.
func (metricas *Metricas) MarcarAntiguedadDelIndice(segundos float64) {
	metricas.antiguedadIndice.Set(segundos)
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
