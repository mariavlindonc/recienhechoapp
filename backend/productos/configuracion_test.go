package main

import (
	"strings"
	"testing"
)

func variablesValidas() map[string]string {
	return map[string]string{
		"MYSQL_DSN":   "productos:clave@tcp(mysql-productos:3306)/productos",
		"REDIS_URL":   "redis://redis-productos:6379/0",
		"MEILI_URL":   "http://meilisearch:7700",
		"MEILI_CLAVE": "clave",
	}
}

func TestLeerConfiguracionConValoresPorDefecto(prueba *testing.T) {
	variables := variablesValidas()

	leida, err := leerConfiguracion(func(nombre string) string { return variables[nombre] })

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if leida.puerto != "8082" || leida.puertoMetricas != "9464" || !leida.cacheHabilitada || leida.carpetaImagenes != "/datos/imagenes" {
		prueba.Errorf("configuración = %+v", leida)
	}
}

func TestSinCacheNoHaceFaltaRedis(prueba *testing.T) {
	variables := variablesValidas()
	delete(variables, "REDIS_URL")
	variables["CACHE_HABILITADA"] = "false"

	leida, err := leerConfiguracion(func(nombre string) string { return variables[nombre] })

	if err != nil || leida.cacheHabilitada {
		prueba.Errorf("con la caché apagada, REDIS_URL no es obligatoria: %+v %v", leida, err)
	}
}

func TestLeerConfiguracionAvisaTodosLosProblemas(prueba *testing.T) {
	variables := map[string]string{"CACHE_HABILITADA": "quizas"}

	_, err := leerConfiguracion(func(nombre string) string { return variables[nombre] })

	for _, problema := range []string{"MYSQL_DSN", "MEILI_URL", "MEILI_CLAVE", "CACHE_HABILITADA"} {
		if err == nil || !strings.Contains(err.Error(), problema) {
			prueba.Errorf("el error no menciona %s: %v", problema, err)
		}
	}
}
