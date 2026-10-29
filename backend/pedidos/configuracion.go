package main

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/adaptadores/inventario"
)

// configuracion reúne todo lo que el servicio lee de las variables de entorno.
type configuracion struct {
	puerto             string
	puertoMetricas     string
	direccionMongo     string
	baseMongo          string
	direccionRabbit    string
	direccionProductos *url.URL
	modoInventario     inventario.Modo
}

// leerConfiguracion recibe la función que busca cada variable (os.Getenv en el
// servicio, un mapa en los tests). Junta todos los problemas en un solo error para
// que se puedan corregir de una vez. Sigue el mismo esquema que notificaciones.
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
		puerto:          conValorPorDefecto("PORT", "8083"),
		puertoMetricas:  conValorPorDefecto("METRICAS_PORT", "9464"),
		direccionMongo:  obligatoria("MONGO_URI"),
		baseMongo:       conValorPorDefecto("MONGO_BASE", "pedidos"),
		direccionRabbit: obligatoria("RABBITMQ_URL"),
		modoInventario:  inventario.Modo(conValorPorDefecto("INVENTARIO_MODO", string(inventario.ModoConfirma))),
	}

	if texto := obligatoria("PRODUCTOS_URL"); texto != "" {
		direccion, err := url.Parse(texto)
		if err != nil || (direccion.Scheme != "http" && direccion.Scheme != "https") || direccion.Host == "" {
			problemas = append(problemas, fmt.Errorf("PRODUCTOS_URL %q inválida: tiene que ser una URL http://host:puerto", texto))
		}
		leida.direccionProductos = direccion
	}
	if !inventario.ModoValido(string(leida.modoInventario)) {
		problemas = append(problemas, fmt.Errorf("INVENTARIO_MODO %q inválido: confirma, sin_stock, lento, error o rechazo", leida.modoInventario))
	}

	if len(problemas) > 0 {
		return configuracion{}, fmt.Errorf("configuración inválida: %w", errors.Join(problemas...))
	}
	return leida, nil
}
