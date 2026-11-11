package main

import (
	"strings"
	"testing"
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
	if leida.puerto != "8083" || leida.baseMongo != "pedidos" || leida.modoInventario != "confirma" {
		prueba.Errorf("configuración = %+v", leida)
	}
	if leida.direccionProductos.Host != "productos:8082" {
		prueba.Errorf("productos = %v", leida.direccionProductos)
	}
	if leida.direccionInventario != nil || leida.baseInventario != "inventario" || leida.sucursalInventario != 1 || len(leida.productosInventario) != 10 {
		prueba.Errorf("sin INVENTARIO_URL se usa el falso, con los valores por defecto: %+v", leida)
	}
}

func TestLeerConfiguracionDelInventarioReal(prueba *testing.T) {
	variables := variablesValidas()
	variables["INVENTARIO_URL"] = "https://stock-volcamelli.onrender.com"
	variables["INVENTARIO_CLAVE_API"] = "una-clave"
	variables["INVENTARIO_PRODUCTOS_IDS"] = "3, 4,5"

	leida, err := leerConfiguracion(func(nombre string) string { return variables[nombre] })

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if leida.direccionInventario.Host != "stock-volcamelli.onrender.com" || leida.claveInventario != "una-clave" || len(leida.productosInventario) != 3 || leida.productosInventario[0] != 3 {
		prueba.Errorf("configuración = %+v", leida)
	}
}

func TestLeerConfiguracionDelInventarioAvisaLoQueFalta(prueba *testing.T) {
	variables := variablesValidas()
	variables["INVENTARIO_URL"] = "https://stock-volcamelli.onrender.com"
	variables["INVENTARIO_SUCURSAL"] = "0"
	variables["INVENTARIO_PRODUCTOS_IDS"] = "1,1"

	_, err := leerConfiguracion(func(nombre string) string { return variables[nombre] })

	for _, problema := range []string{"INVENTARIO_CLAVE_API", "INVENTARIO_SUCURSAL", "INVENTARIO_PRODUCTOS_IDS"} {
		if err == nil || !strings.Contains(err.Error(), problema) {
			prueba.Errorf("el error no menciona %s: %v", problema, err)
		}
	}
}

func TestLeerConfiguracionAvisaTodosLosProblemas(prueba *testing.T) {
	variables := map[string]string{"PRODUCTOS_URL": "productos:8082", "INVENTARIO_MODO": "roto"}

	_, err := leerConfiguracion(func(nombre string) string { return variables[nombre] })

	for _, problema := range []string{"MONGO_URI", "RABBITMQ_URL", "PRODUCTOS_URL", "INVENTARIO_MODO"} {
		if err == nil || !strings.Contains(err.Error(), problema) {
			prueba.Errorf("el error no menciona %s: %v", problema, err)
		}
	}
}
