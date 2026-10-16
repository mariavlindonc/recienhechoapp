package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	nombreServicio = "notificaciones"
	esperaMongo    = 2 * time.Second
)

func main() {
	puerto := os.Getenv("PORT")
	if puerto == "" {
		puerto = "8084"
	}

	direccionMongo := os.Getenv("MONGO_URI")
	if direccionMongo == "" {
		log.Fatal("falta la variable de entorno MONGO_URI")
	}
	clienteMongo, err := mongo.Connect(options.Client().ApplyURI(direccionMongo))
	if err != nil {
		log.Fatalf("configuración de Mongo inválida: %v", err)
	}

	router := gin.Default()
	router.GET("/health", func(contexto *gin.Context) {
		contextoEspera, cancelar := context.WithTimeout(contexto.Request.Context(), esperaMongo)
		defer cancelar()
		if err := clienteMongo.Ping(contextoEspera, nil); err != nil {
			log.Printf("health: Mongo no responde: %v", err)
			contexto.JSON(http.StatusServiceUnavailable, gin.H{"status": "degradado", "service": nombreServicio, "mongo": "caido"})
			return
		}
		contexto.JSON(http.StatusOK, gin.H{"status": "ok", "service": nombreServicio, "mongo": "ok"})
	})

	if err := router.Run(":" + puerto); err != nil {
		log.Fatal(err)
	}
}
