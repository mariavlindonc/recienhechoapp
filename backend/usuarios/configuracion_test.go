package main

import (
	"strings"
	"testing"
)

func variablesValidas() map[string]string {
	return map[string]string{
		"MYSQL_DSN":      "usuarios:clave@tcp(mysql-usuarios:3306)/usuarios",
		"JWT_SECRETO":    strings.Repeat("s", largoMinimoSecreto),
		"VENDEDOR_EMAIL": "vendedor@recienhecho.test",
		"VENDEDOR_CLAVE": "una-clave-de-desarrollo",
	}
}

func TestLeerConfiguracionConValoresPorDefecto(prueba *testing.T) {
	variables := variablesValidas()

	leida, err := leerConfiguracion(func(nombre string) string { return variables[nombre] })

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if leida.puerto != "8081" || leida.puertoMetricas != "9464" {
		prueba.Errorf("configuración = %+v", leida)
	}
}

func TestSecretoCortoSeRechaza(prueba *testing.T) {
	variables := variablesValidas()
	variables["JWT_SECRETO"] = "corto"

	_, err := leerConfiguracion(func(nombre string) string { return variables[nombre] })

	if err == nil || !strings.Contains(err.Error(), "JWT_SECRETO") {
		prueba.Errorf("un secreto de 5 bytes tiene que rechazarse: %v", err)
	}
}

func TestLeerConfiguracionAvisaTodosLosProblemas(prueba *testing.T) {
	_, err := leerConfiguracion(func(string) string { return "" })

	for _, problema := range []string{"MYSQL_DSN", "JWT_SECRETO", "VENDEDOR_EMAIL", "VENDEDOR_CLAVE"} {
		if err == nil || !strings.Contains(err.Error(), problema) {
			prueba.Errorf("el error no menciona %s: %v", problema, err)
		}
	}
}
