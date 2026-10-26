package main

import (
	"strings"
	"testing"
	"time"
)

func variablesValidas() map[string]string {
	return map[string]string{
		"MONGO_URI":     "mongodb://mongo-pedidos:27017",
		"RABBITMQ_URL":  "amqp://rabbitmq:5672",
		"PRODUCTOS_URL": "http://productos:8082",
	}
}

func TestLeerConfiguracionConValoresPorDefecto(prueba *testing.T) {
	variables := variablesValidas()

	leida, err := leerConfiguracion(func(nombre string) string { return variables[nombre] })

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if leida.puerto != "8083" || leida.baseMongo != "pedidos" || leida.modoInventario != "confirma" || leida.intervalo != time.Second {
		prueba.Errorf("configuración = %+v", leida)
	}
	if leida.direccionProductos.Host != "productos:8082" {
		prueba.Errorf("productos = %v", leida.direccionProductos)
	}
}

func TestLeerConfiguracionAvisaTodosLosProblemas(prueba *testing.T) {
	variables := map[string]string{"PRODUCTOS_URL": "productos:8082", "INVENTARIO_MODO": "roto", "PEDIDOS_INTERVALO": "nunca"}

	_, err := leerConfiguracion(func(nombre string) string { return variables[nombre] })

	for _, problema := range []string{"MONGO_URI", "RABBITMQ_URL", "PRODUCTOS_URL", "INVENTARIO_MODO", "PEDIDOS_INTERVALO"} {
		if err == nil || !strings.Contains(err.Error(), problema) {
			prueba.Errorf("el error no menciona %s: %v", problema, err)
		}
	}
}
