package main

import (
	"errors"
	"fmt"
)

// largoMinimoSecreto es el mínimo para HS256: 32 bytes, el tamaño de su hash.
const largoMinimoSecreto = 32

// configuracion reúne todo lo que el servicio lee de las variables de entorno.
type configuracion struct {
	puerto         string
	puertoMetricas string
	dsnMySQL       string
	secretoJWT     string
	emailVendedor  string
	claveVendedor  string
}

// leerConfiguracion recibe la función que busca cada variable (os.Getenv en el
// servicio, un mapa en los tests). Junta todos los problemas en un solo error para
// que se puedan corregir de una vez. Sigue el mismo esquema que productos.
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
		puerto:         conValorPorDefecto("PORT", "8081"),
		puertoMetricas: conValorPorDefecto("METRICAS_PORT", "9464"),
		dsnMySQL:       obligatoria("MYSQL_DSN"),
		secretoJWT:     obligatoria("JWT_SECRETO"),
		emailVendedor:  obligatoria("VENDEDOR_EMAIL"),
		claveVendedor:  obligatoria("VENDEDOR_CLAVE"),
	}
	if leida.secretoJWT != "" && len(leida.secretoJWT) < largoMinimoSecreto {
		problemas = append(problemas, fmt.Errorf("JWT_SECRETO tiene que tener al menos %d bytes", largoMinimoSecreto))
	}

	if len(problemas) > 0 {
		return configuracion{}, fmt.Errorf("configuración inválida: %w", errors.Join(problemas...))
	}
	return leida, nil
}
