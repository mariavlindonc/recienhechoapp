package main

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func variablesMinimas() map[string]string {
	return map[string]string{
		"MONGO_URI":                 "mongodb://localhost:27017",
		"SMTP_HOST":                 "mailpit",
		"SMTP_REMITENTE":            "Recién hecho <avisos@recienhecho.test>",
		"NOTIFICACIONES_CLAVES_API": "grupo-a:clave-a",
	}
}

func TestLeerConfiguracionUsaValoresPorDefecto(prueba *testing.T) {
	variables := variablesMinimas()

	leida, err := leerConfiguracion(func(nombre string) string { return variables[nombre] })

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if leida.puerto != "8084" || leida.puertoMetricas != "9464" || leida.baseMongo != "notificaciones" || leida.limitePorMinuto != 60 {
		prueba.Errorf("valores por defecto incorrectos: %+v", leida)
	}
	if leida.intervaloDeEnvio != 5*time.Second || leida.smtp.Puerto != 587 || leida.smtp.TiempoMaximo != 10*time.Second || leida.smtp.TiempoMaximoConexion != 2*time.Second {
		prueba.Errorf("valores por defecto incorrectos: %+v", leida)
	}
	if leida.clavesAPI["clave-a"] != "grupo-a" {
		prueba.Errorf("claves = %v", leida.clavesAPI)
	}
}

func TestLeerConfiguracionInformaTodosLosProblemas(prueba *testing.T) {
	variables := map[string]string{"SMTP_PUERTO": "uno", "NOTIFICACIONES_INTERVALO_DE_ENVIO": "-1s"}

	_, err := leerConfiguracion(func(nombre string) string { return variables[nombre] })

	if err == nil {
		prueba.Fatal("se esperaba un error")
	}
	for _, problema := range []string{"MONGO_URI", "SMTP_HOST", "SMTP_REMITENTE", "NOTIFICACIONES_CLAVES_API", "SMTP_PUERTO", "NOTIFICACIONES_INTERVALO_DE_ENVIO"} {
		if !strings.Contains(err.Error(), problema) {
			prueba.Errorf("el error no menciona %s: %v", problema, err)
		}
	}
}

func TestNuevoUUIDTieneFormatoVersion4(prueba *testing.T) {
	formato := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	vistos := map[string]bool{}

	for range 1000 {
		id := nuevoUUID()
		if !formato.MatchString(id) {
			prueba.Fatalf("%q no es un UUID v4", id)
		}
		if vistos[id] {
			prueba.Fatalf("%q se repitió", id)
		}
		vistos[id] = true
	}
}
