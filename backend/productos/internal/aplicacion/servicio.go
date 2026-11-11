// Package aplicacion tiene los casos de uso de productos: el ABM de categorías y
// productos, la receta y la imagen, el catálogo con caché, la búsqueda y la
// sincronización del índice. Coordina: le ordena al dominio que decida, guarda por
// los puertos e invalida la caché. Cada tema está en su archivo.
package aplicacion

import (
	"context"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
)

// Servicio reúne los casos de uso.
type Servicio struct {
	categorias puertos.Categorias
	productos  puertos.Productos
	bandeja    puertos.BandejaDelIndice
	buscador   puertos.Buscador
	cache      puertos.CacheDelCatalogo
	imagenes   puertos.AlmacenDeImagenes
	eventos    puertos.Eventos
	ahora      func() time.Time
	nuevoID    func() string
}

// Dependencias del servicio. El reloj y el generador de ids se reciben de afuera
// para que los tests puedan controlarlos.
type Dependencias struct {
	Categorias puertos.Categorias
	Productos  puertos.Productos
	Bandeja    puertos.BandejaDelIndice
	Buscador   puertos.Buscador
	Cache      puertos.CacheDelCatalogo
	Imagenes   puertos.AlmacenDeImagenes
	Eventos    puertos.Eventos
	Ahora      func() time.Time
	NuevoID    func() string
}

// NuevoServicio arma el servicio.
func NuevoServicio(dependencias Dependencias) *Servicio {
	return &Servicio{
		categorias: dependencias.Categorias,
		productos:  dependencias.Productos,
		bandeja:    dependencias.Bandeja,
		buscador:   dependencias.Buscador,
		cache:      dependencias.Cache,
		imagenes:   dependencias.Imagenes,
		eventos:    dependencias.Eventos,
		ahora:      dependencias.Ahora,
		nuevoID:    dependencias.NuevoID,
	}
}

// conCache lee primero de la caché y, si no está, de la base, y guarda lo leído
// (cache-aside, ADR-009). Si la caché falla, se sigue con la base: el comprador
// solo nota la demora.
func conCache[Valor any](
	contexto context.Context,
	servicio *Servicio,
	tipo string,
	deLaCache func(context.Context) (Valor, bool, error),
	deLaBase func(context.Context) (Valor, error),
	guardar func(context.Context, Valor) error,
) (Valor, error) {
	valor, encontrado, err := deLaCache(contexto)
	cacheResponde := err == nil
	if cacheResponde {
		servicio.eventos.CacheConsultada(contexto, tipo, encontrado)
		if encontrado {
			return valor, nil
		}
	} else {
		servicio.eventos.CacheFallo(contexto, "leer_"+tipo, err)
	}

	valor, err = deLaBase(contexto)
	if err != nil {
		var vacio Valor
		return vacio, err
	}
	// Si la caché no respondió al leer, no se la espera otra vez para guardar.
	if cacheResponde {
		if err := guardar(contexto, valor); err != nil {
			servicio.eventos.CacheFallo(contexto, "guardar_"+tipo, err)
		}
	}
	return valor, nil
}

// invalidarCatalogo olvida las páginas del catálogo y la lista de categorías. Se
// llama después de guardar: si la caché no responde, lo viejo vence solo por
// vigencia (ADR-009).
func (servicio *Servicio) invalidarCatalogo(contexto context.Context) {
	if err := servicio.cache.InvalidarCatalogo(contexto); err != nil {
		servicio.eventos.CacheFallo(contexto, "invalidar_catalogo", err)
	}
}

// invalidarProducto olvida el detalle del producto y el catálogo, donde también aparece.
func (servicio *Servicio) invalidarProducto(contexto context.Context, id string) {
	if err := servicio.cache.InvalidarProducto(contexto, id); err != nil {
		servicio.eventos.CacheFallo(contexto, "invalidar_producto", err)
	}
	servicio.invalidarCatalogo(contexto)
}
