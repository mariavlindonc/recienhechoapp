package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

const nombreServicio = "notificaciones"

func main() {
	puerto := os.Getenv("PORT")
	if puerto == "" {
		puerto = "8084"
	}

	router := gin.Default()
	router.GET("/health", func(contexto *gin.Context) {
		contexto.JSON(http.StatusOK, gin.H{"status": "ok", "service": nombreServicio})
	})

	if err := router.Run(":" + puerto); err != nil {
		log.Fatal(err)
	}
}
