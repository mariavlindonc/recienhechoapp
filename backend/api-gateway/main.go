package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

const nombreServicio = "api-gateway"

// tiempoMaximoServicios es el timeout del gateway hacia los servicios internos (ADR-003).
const tiempoMaximoServicios = 3 * time.Second

// esperaTrazasAlApagar es cuánto se espera para mandar los últimos spans.
const esperaTrazasAlApagar = 5 * time.Second

func main() {
	// Todos los logs del gateway salen en JSON con el nombre del servicio (ADR-006).
	slog.SetDefault(nuevoLogger(os.Stdout, nombreServicio))

	if err := ejecutar(); err != nil {
		slog.Error("el api-gateway terminó con error", "error", err)
		os.Exit(1)
	}
}

func ejecutar() error {
	puerto := os.Getenv("PORT")
	if puerto == "" {
		puerto = "8080"
	}
	instanciasNotificaciones, err := parsearInstancias(os.Getenv("NOTIFICACIONES_URLS"))
	if err != nil {
		return fmt.Errorf("NOTIFICACIONES_URLS: %w", err)
	}

	apagarTrazas, err := iniciarTrazas(context.Background(), nombreServicio)
	if err != nil {
		return err
	}
	defer func() {
		contextoTrazas, cancelarTrazas := context.WithTimeout(context.Background(), esperaTrazasAlApagar)
		defer cancelarTrazas()
		apagarTrazas(contextoTrazas)
	}()

	medidor := nuevasMetricas()
	go func() {
		// Sin métricas el gateway sigue atendiendo: el problema queda en el log.
		if err := medidor.servir(context.Background(), puertoMetricas()); err != nil {
			slog.Error("no se pudieron publicar las métricas", "error", err)
		}
	}()

	proxyNotificaciones := nuevoProxyBalanceado("notificaciones", instanciasNotificaciones, tiempoMaximoServicios, medidor.contarReenvio("notificaciones"))
	router := nuevoRouter(proxyNotificaciones, medidor.medirSolicitudes())
	slog.Info("api-gateway escuchando", "puerto", puerto, "instancias_notificaciones", len(instanciasNotificaciones))
	if err := router.Run(":" + puerto); err != nil {
		return fmt.Errorf("servidor HTTP: %w", err)
	}
	return nil
}

// nuevoRouter arma las rutas públicas. Todo lo que empieza con /notificaciones va
// al servicio de notificaciones tal cual llega (headers, cuerpo y respuesta).
func nuevoRouter(notificaciones http.Handler, extras ...gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	// otelgin va primero: abre el span de la solicitud y así el middleware de logs
	// ya ve el trace_id.
	router.Use(otelgin.Middleware(nombreServicio, otelgin.WithFilter(sinChequeosDeSalud)), gin.Recovery(), registrarSolicitudes(slog.Default()))
	router.Use(extras...)
	router.GET("/health", func(contexto *gin.Context) {
		contexto.JSON(http.StatusOK, gin.H{"status": "ok", "service": nombreServicio})
	})
	router.Any("/notificaciones", gin.WrapH(notificaciones))
	router.Any("/notificaciones/*resto", gin.WrapH(notificaciones))
	return router
}

// sinChequeosDeSalud evita trazar /health, que no aporta nada en Jaeger.
func sinChequeosDeSalud(pedido *http.Request) bool {
	return pedido.URL.Path != "/health"
}

// puertoMetricas es el puerto interno donde se publica /metrics para Prometheus.
func puertoMetricas() string {
	if puerto := os.Getenv("METRICAS_PORT"); puerto != "" {
		return puerto
	}
	return "9464"
}
