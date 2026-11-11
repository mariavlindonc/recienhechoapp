package main

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/adaptadores/inventario"
)

// configuracion reúne todo lo que el servicio lee de las variables de entorno.
type configuracion struct {
	puerto             string
	puertoMetricas     string
	direccionMongo     string
	baseMongo          string
	baseInventario     string
	direccionRabbit    string
	direccionProductos *url.URL
	modoInventario     inventario.Modo
	// direccionInventario es la del proveedor real; si no está, se usa el inventario
	// falso en modoInventario (ADR-020).
	direccionInventario *url.URL
	claveInventario     string
	sucursalInventario  int64
	// productosInventario son los productos del proveedor que se usan de casillero
	// de un ingrediente cada uno.
	productosInventario []int64
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
		baseInventario:  conValorPorDefecto("MONGO_BASE_INVENTARIO", "inventario"),
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

	if texto := buscar("INVENTARIO_URL"); texto != "" {
		direccion, err := url.Parse(texto)
		if err != nil || (direccion.Scheme != "http" && direccion.Scheme != "https") || direccion.Host == "" {
			problemas = append(problemas, fmt.Errorf("INVENTARIO_URL %q inválida: tiene que ser una URL https://host", texto))
		}
		leida.direccionInventario = direccion
		leida.claveInventario = obligatoria("INVENTARIO_CLAVE_API")
	}
	sucursal, err := strconv.ParseInt(conValorPorDefecto("INVENTARIO_SUCURSAL", "1"), 10, 64)
	if err != nil || sucursal < 1 {
		problemas = append(problemas, fmt.Errorf("INVENTARIO_SUCURSAL inválida: tiene que ser un número mayor a 0"))
	}
	leida.sucursalInventario = sucursal

	productos, err := leerProductos(conValorPorDefecto("INVENTARIO_PRODUCTOS_IDS", "1,2,3,4,5,6,7,8,9,10"))
	if err != nil {
		problemas = append(problemas, fmt.Errorf("INVENTARIO_PRODUCTOS_IDS inválida: %w", err))
	}
	leida.productosInventario = productos

	if len(problemas) > 0 {
		return configuracion{}, fmt.Errorf("configuración inválida: %w", errors.Join(problemas...))
	}
	return leida, nil
}

// leerProductos lee una lista de ids separados por coma, sin repetidos.
func leerProductos(texto string) ([]int64, error) {
	var ids []int64
	vistos := map[int64]bool{}
	for _, parte := range strings.Split(texto, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(parte), 10, 64)
		if err != nil || id < 1 {
			return nil, fmt.Errorf("%q no es un id mayor a 0", parte)
		}
		if vistos[id] {
			return nil, fmt.Errorf("el id %d está repetido", id)
		}
		vistos[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}
