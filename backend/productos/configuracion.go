package main

import (
	"errors"
)

// configuracion reúne todo lo que el servicio lee de las variables de entorno.
type configuracion struct {
	puerto         string
	puertoMetricas string
}

// leerConfiguracion recibe la función que busca cada variable (os.Getenv en el
// servicio, un mapa en los tests). Junta todos los problemas en un solo error para
// que se puedan corregir de una vez. Sigue el mismo esquema que pedidos.
func leerConfiguracion(buscar func(nombre string) string) (configuracion, error) {
	var problemas []error
	conValorPorDefecto := func(nombre, porDefecto string) string {
		if valor := buscar(nombre); valor != "" {
			return valor
		}
		return porDefecto
	}

	leida := configuracion{
		puerto:         conValorPorDefecto("PORT", "8082"),
		puertoMetricas: conValorPorDefecto("METRICAS_PORT", "9464"),
	}

	if len(problemas) > 0 {
		return configuracion{}, errors.Join(problemas...)
	}
	return leida, nil
}
