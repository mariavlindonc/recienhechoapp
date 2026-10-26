package aplicacion

import (
	"context"
	"fmt"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
)

// CrearCategoria da de alta una categoría del catálogo.
func (servicio *Servicio) CrearCategoria(contexto context.Context, nombre string) (dominio.Categoria, error) {
	categoria, err := dominio.NuevaCategoria(servicio.nuevoID(), nombre, servicio.ahora())
	if err != nil {
		return dominio.Categoria{}, err
	}
	if err := servicio.categorias.CrearCategoria(contexto, categoria); err != nil {
		return dominio.Categoria{}, fmt.Errorf("crear la categoría: %w", err)
	}
	servicio.invalidarCatalogo(contexto)
	return categoria, nil
}

// RenombrarCategoria cambia el nombre. Sus productos se vuelven a indexar, porque
// la búsqueda encuentra un producto también por el nombre de su categoría.
//
// shortcut: el detalle de cada producto en la caché muestra el nombre viejo hasta
// que vence (60 s). Invalidar uno por uno exige listar los productos de la
// categoría; se hace si molesta en la demo.
func (servicio *Servicio) RenombrarCategoria(contexto context.Context, id, nombre string) (dominio.Categoria, error) {
	categoria, err := servicio.categorias.BuscarCategoria(contexto, id)
	if err != nil {
		return dominio.Categoria{}, fmt.Errorf("buscar la categoría: %w", err)
	}
	renombrada, err := categoria.Renombrar(nombre)
	if err != nil {
		return dominio.Categoria{}, err
	}
	if err := servicio.categorias.RenombrarCategoria(contexto, renombrada); err != nil {
		return dominio.Categoria{}, fmt.Errorf("renombrar la categoría: %w", err)
	}
	servicio.invalidarCatalogo(contexto)
	return renombrada, nil
}

// EliminarCategoria da de baja una categoría sin productos.
func (servicio *Servicio) EliminarCategoria(contexto context.Context, id string) error {
	if err := servicio.categorias.EliminarCategoria(contexto, id); err != nil {
		return fmt.Errorf("eliminar la categoría: %w", err)
	}
	servicio.invalidarCatalogo(contexto)
	return nil
}

// ListarCategorias devuelve las categorías para los filtros del catálogo, con caché.
func (servicio *Servicio) ListarCategorias(contexto context.Context) ([]dominio.Categoria, error) {
	return conCache(contexto, servicio, "categorias",
		servicio.cache.Categorias,
		servicio.categorias.ListarCategorias,
		servicio.cache.GuardarCategorias,
	)
}
