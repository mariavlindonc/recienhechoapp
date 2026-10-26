// Package api expone los casos de uso de productos por HTTP. Solo se llega por el
// api-gateway (ADR-001).
package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

const nombreServicio = "productos"

// Registrar agrega las rutas al router. chequearSalud dice si las dependencias
// obligatorias responden (MySQL).
func Registrar(router gin.IRouter, chequearSalud func(contexto context.Context) error) {
	router.GET("/health", salud(chequearSalud))
	router.GET("/vivo", vivo)
}

func salud(chequear func(contexto context.Context) error) gin.HandlerFunc {
	return func(contexto *gin.Context) {
		if chequear != nil {
			if err := chequear(contexto.Request.Context()); err != nil {
				slog.WarnContext(contexto.Request.Context(), "chequeo de salud fallido", "error", err)
				contexto.JSON(http.StatusServiceUnavailable, gin.H{"status": "degradado", "service": nombreServicio})
				return
			}
		}
		contexto.JSON(http.StatusOK, gin.H{"status": "ok", "service": nombreServicio})
	}
}

// vivo responde si el proceso atiende, sin mirar dependencias (ADR-012).
func vivo(contexto *gin.Context) {
	contexto.JSON(http.StatusOK, gin.H{"status": "vivo", "service": nombreServicio})
}
