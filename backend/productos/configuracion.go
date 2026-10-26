package main

import (
	"errors"
	"fmt"
	"strconv"
)

// configuracion reúne todo lo que el servicio lee de las variables de entorno.
type configuracion struct {
	puerto          string
	puertoMetricas  string
	dsnMySQL        string
	direccionRedis  string
	cacheHabilitada bool
	direccionMeili  string
	claveMeili      string
	carpetaImagenes string
}

// leerConfiguracion recibe la función que busca cada variable (os.Getenv en el
// servicio, un mapa en los tests). Junta todos los problemas en un solo error para
// que se puedan corregir de una vez. Sigue el mismo esquema que pedidos.
func leerConfiguracion(buscar func(nombre string) string) (configuracion, error) {
	var problemas []error
	obligatoria := func(nombre string) string {
		valor := buscar(nombre)
		if valor == "" {
			problemas = append(problemas, fmt.Errorf("falta la variable %s", nombre))
		}
		return valor
	}
	conValorPorDefecto := func(nombre, porDefecto string) string {
		if valor := buscar(nombre); valor != "" {
			return valor
		}
		return porDefecto
	}

	leida := configuracion{
		puerto:          conValorPorDefecto("PORT", "8082"),
		puertoMetricas:  conValorPorDefecto("METRICAS_PORT", "9464"),
		dsnMySQL:        obligatoria("MYSQL_DSN"),
		direccionMeili:  obligatoria("MEILI_URL"),
		claveMeili:      obligatoria("MEILI_CLAVE"),
		carpetaImagenes: conValorPorDefecto("IMAGENES_DIR", "/datos/imagenes"),
	}

	// CACHE_HABILITADA=false apaga la caché para medir el catálogo sin ella (ADR-009).
	cache, err := strconv.ParseBool(conValorPorDefecto("CACHE_HABILITADA", "true"))
	if err != nil {
		problemas = append(problemas, fmt.Errorf("CACHE_HABILITADA %q inválida: true o false", buscar("CACHE_HABILITADA")))
	}
	leida.cacheHabilitada = cache
	if leida.cacheHabilitada {
		leida.direccionRedis = obligatoria("REDIS_URL")
	}

	if len(problemas) > 0 {
		return configuracion{}, fmt.Errorf("configuración inválida: %w", errors.Join(problemas...))
	}
	return leida, nil
}
