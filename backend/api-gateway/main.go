package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

const nombreServicio = "api-gateway"

// tiempoMaximoServicios es el timeout del gateway hacia los servicios internos (ADR-003).
const tiempoMaximoServicios = 3 * time.Second

func main() {
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

	router := nuevoRouter(nuevoProxyBalanceado("notificaciones", instanciasNotificaciones, tiempoMaximoServicios))
	slog.Info("api-gateway escuchando", "puerto", puerto, "instancias_notificaciones", len(instanciasNotificaciones))
	if err := router.Run(":" + puerto); err != nil {
		return fmt.Errorf("servidor HTTP: %w", err)
	}
	return nil
}

// nuevoRouter arma las rutas públicas. Todo lo que empieza con /notificaciones va
// al servicio de notificaciones tal cual llega (headers, cuerpo y respuesta).
func nuevoRouter(notificaciones http.Handler) *gin.Engine {
	router := gin.Default()
	router.GET("/health", func(contexto *gin.Context) {
		contexto.JSON(http.StatusOK, gin.H{"status": "ok", "service": nombreServicio})
	})
	router.Any("/notificaciones", gin.WrapH(notificaciones))
	router.Any("/notificaciones/*resto", gin.WrapH(notificaciones))
	return router
}
