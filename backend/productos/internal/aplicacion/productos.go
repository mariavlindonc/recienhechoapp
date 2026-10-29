package aplicacion

import (
	"context"
	"fmt"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
)

// CrearProducto da de alta un producto (VE1). Queda en la bandeja del índice y
// aparece en la búsqueda en pocos segundos (RNF1).
func (servicio *Servicio) CrearProducto(contexto context.Context, datos dominio.DatosProducto) (dominio.Producto, error) {
	producto, err := dominio.NuevoProducto(servicio.nuevoID(), datos, servicio.ahora())
	if err != nil {
		return dominio.Producto{}, err
	}
	guardado, err := servicio.productos.CrearProducto(contexto, producto)
	if err != nil {
		return dominio.Producto{}, fmt.Errorf("crear el producto: %w", err)
	}
	servicio.invalidarCatalogo(contexto)
	return guardado, nil
}

// ModificarProducto cambia los datos del producto (VE1). Los pedidos ya hechos no
// cambian: guardan su propia copia del precio (R4).
func (servicio *Servicio) ModificarProducto(contexto context.Context, id string, datos dominio.DatosProducto) (dominio.Producto, error) {
	actual, err := servicio.productos.BuscarProducto(contexto, id)
	if err != nil {
		return dominio.Producto{}, fmt.Errorf("buscar el producto: %w", err)
	}
	modificado, err := actual.Modificar(datos, servicio.ahora())
	if err != nil {
		return dominio.Producto{}, err
	}
	guardado, err := servicio.productos.ModificarProducto(contexto, modificado)
	if err != nil {
		return dominio.Producto{}, fmt.Errorf("modificar el producto: %w", err)
	}
	servicio.invalidarProducto(contexto, id)
	return guardado, nil
}

// ReemplazarReceta carga la receta del producto (VE2). La caché no se toca: la
// vista pública no muestra la receta, y pedidos la lee siempre de la base.
func (servicio *Servicio) ReemplazarReceta(contexto context.Context, id string, receta []dominio.Insumo) (dominio.Producto, error) {
	actual, err := servicio.productos.BuscarProducto(contexto, id)
	if err != nil {
		return dominio.Producto{}, fmt.Errorf("buscar el producto: %w", err)
	}
	conReceta, err := actual.ReemplazarReceta(receta, servicio.ahora())
	if err != nil {
		return dominio.Producto{}, err
	}
	if err := servicio.productos.GuardarReceta(contexto, conReceta); err != nil {
		return dominio.Producto{}, fmt.Errorf("guardar la receta: %w", err)
	}
	return conReceta, nil
}

// SubirImagen guarda la foto del producto. Si no se puede guardar en la base, se
// borra el archivo nuevo para no dejarlo suelto.
//
// shortcut: la foto anterior queda en el volumen. Borrarla rompería la imagen
// donde todavía se la muestra (la búsqueda tarda unos segundos en actualizarse y
// la caché hasta 60 s). Si el volumen crece, se limpian los archivos que ningún
// producto usa.
func (servicio *Servicio) SubirImagen(contexto context.Context, id string, contenido []byte) (dominio.Producto, error) {
	imagen, err := dominio.NuevaImagen(contenido)
	if err != nil {
		return dominio.Producto{}, err
	}
	actual, err := servicio.productos.BuscarProducto(contexto, id)
	if err != nil {
		return dominio.Producto{}, fmt.Errorf("buscar el producto: %w", err)
	}
	url, err := servicio.imagenes.Guardar(contexto, imagen)
	if err != nil {
		return dominio.Producto{}, fmt.Errorf("guardar el archivo de la imagen: %w", err)
	}
	conImagen := actual.CambiarImagen(url, servicio.ahora())
	if err := servicio.productos.GuardarImagen(contexto, conImagen); err != nil {
		if errBorrar := servicio.imagenes.Borrar(contexto, url); errBorrar != nil {
			servicio.eventos.ImagenSinBorrar(contexto, url, errBorrar)
		}
		return dominio.Producto{}, fmt.Errorf("guardar la imagen del producto: %w", err)
	}
	servicio.invalidarProducto(contexto, id)
	return conImagen, nil
}

// EliminarProducto da de baja el producto (baja lógica). Sale del catálogo y de la
// búsqueda; la imagen se conserva por si se recupera.
func (servicio *Servicio) EliminarProducto(contexto context.Context, id string) error {
	if err := servicio.productos.EliminarProducto(contexto, id, servicio.ahora()); err != nil {
		return fmt.Errorf("eliminar el producto: %w", err)
	}
	servicio.invalidarProducto(contexto, id)
	return nil
}

// ObtenerProducto devuelve el producto como lo ve el comprador, con caché.
func (servicio *Servicio) ObtenerProducto(contexto context.Context, id string) (dominio.Producto, error) {
	return conCache(contexto, servicio, "producto",
		func(contexto context.Context) (dominio.Producto, bool, error) {
			return servicio.cache.Producto(contexto, id)
		},
		func(contexto context.Context) (dominio.Producto, error) {
			producto, err := servicio.productos.BuscarProducto(contexto, id)
			return producto.VistaPublica(), err
		},
		servicio.cache.GuardarProducto,
	)
}

// ObtenerProductoConReceta devuelve el producto completo, siempre de la base: lo
// piden el vendedor y pedidos, que cobra este precio (ADR-009).
func (servicio *Servicio) ObtenerProductoConReceta(contexto context.Context, id string) (dominio.Producto, error) {
	return servicio.productos.BuscarProducto(contexto, id)
}

// ListarProductos devuelve una página del catálogo (C2). La del comprador sale de
// la caché; la del vendedor, que incluye lo no disponible, de la base.
func (servicio *Servicio) ListarProductos(contexto context.Context, categoriaID string, incluirNoDisponibles bool, pagina int) (dominio.PaginaDeProductos, error) {
	filtro, err := dominio.NuevoFiltroDeCatalogo(categoriaID, incluirNoDisponibles, pagina)
	if err != nil {
		return dominio.PaginaDeProductos{}, err
	}
	if !filtro.SeCachea() {
		return servicio.productos.ListarProductos(contexto, filtro)
	}
	return conCache(contexto, servicio, "catalogo",
		func(contexto context.Context) (dominio.PaginaDeProductos, bool, error) {
			return servicio.cache.Pagina(contexto, filtro)
		},
		func(contexto context.Context) (dominio.PaginaDeProductos, error) {
			return servicio.productos.ListarProductos(contexto, filtro)
		},
		func(contexto context.Context, pagina dominio.PaginaDeProductos) error {
			return servicio.cache.GuardarPagina(contexto, filtro, pagina)
		},
	)
}
