package main

import "testing"

func TestLeerConfiguracionConValoresPorDefecto(prueba *testing.T) {
	leida, err := leerConfiguracion(func(string) string { return "" })

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if leida.puerto != "8082" || leida.puertoMetricas != "9464" {
		prueba.Errorf("configuración = %+v", leida)
	}
}
