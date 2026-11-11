// Package puertos define, como interfaces, lo que la aplicación necesita del
// exterior (ADR-007). Hay un puerto por tema, y cada uno dice qué se quiere, no
// cómo lo resuelve la tecnología: las claves de Redis, el SQL o la API de
// Meilisearch quedan en los adaptadores.
package puertos

import (
	"context"
	"errors"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
)

// ErrBusquedaNoDisponible lo devuelve el Buscador cuando el motor no responde: la
// búsqueda de texto se pierde, pero el catálogo sigue andando (ADR-008).
var ErrBusquedaNoDisponible = errors.New("la búsqueda no está disponible")

// Categorias guarda las categorías del catálogo.
type Categorias interface {
	// CrearCategoria devuelve dominio.ErrCategoriaRepetida si ya hay una con ese nombre.
	CrearCategoria(contexto context.Context, categoria dominio.Categoria) error
	// BuscarCategoria devuelve dominio.ErrCategoriaNoEncontrada si no existe.
	BuscarCategoria(contexto context.Context, id string) (dominio.Categoria, error)
	// RenombrarCategoria guarda el nombre nuevo y, en la misma transacción, manda a
	// la bandeja del índice todos los productos de la categoría, porque el índice
	// guarda el nombre. Devuelve dominio.ErrCategoriaRepetida.
	RenombrarCategoria(contexto context.Context, categoria dominio.Categoria) error
	// EliminarCategoria es una baja lógica. Devuelve dominio.ErrCategoriaEnUso si
	// tiene productos y dominio.ErrCategoriaNoEncontrada si no existe.
	EliminarCategoria(contexto context.Context, id string) error
	// ListarCategorias las devuelve ordenadas por nombre.
	ListarCategorias(contexto context.Context) ([]dominio.Categoria, error)
}

// Productos guarda el catálogo. Cada cambio deja, en la misma transacción, la fila
// de la bandeja del índice del producto: o se guarda todo o nada (ADR-008).
type Productos interface {
	// CrearProducto devuelve dominio.ErrCategoriaNoEncontrada si la categoría no
	// existe. El producto que vuelve trae el nombre de la categoría.
	CrearProducto(contexto context.Context, producto dominio.Producto) (dominio.Producto, error)
	// ModificarProducto guarda los datos del producto (no su receta ni su imagen).
	// Devuelve dominio.ErrNoEncontrado o dominio.ErrCategoriaNoEncontrada.
	ModificarProducto(contexto context.Context, producto dominio.Producto) (dominio.Producto, error)
	// GuardarImagen guarda la URL de la imagen del producto.
	GuardarImagen(contexto context.Context, producto dominio.Producto) error
	// GuardarReceta reemplaza la receta guardada por la del producto.
	GuardarReceta(contexto context.Context, producto dominio.Producto) error
	// EliminarProducto es una baja lógica: deja de aparecer en todas las consultas.
	// Devuelve dominio.ErrNoEncontrado.
	EliminarProducto(contexto context.Context, id string, ahora time.Time) error
	// BuscarProducto devuelve el producto con su receta, o dominio.ErrNoEncontrado.
	BuscarProducto(contexto context.Context, id string) (dominio.Producto, error)
	// ListarProductos devuelve una página del catálogo ordenada por nombre, sin recetas.
	ListarProductos(contexto context.Context, filtro dominio.FiltroDeCatalogo) (dominio.PaginaDeProductos, error)
}

// BandejaDelIndice es la bandeja de salida del índice de búsqueda (ADR-008): los
// productos que cambiaron y todavía no se reflejaron en la búsqueda.
type BandejaDelIndice interface {
	// CambiosPendientes devuelve hasta `limite` filas, de la más vieja a la más nueva.
	CambiosPendientes(contexto context.Context, limite int) (CambiosDeIndice, error)
	// QuitarCambios borra las filas que ya se reflejaron en el índice.
	QuitarCambios(contexto context.Context, ids []int64) error
	// ProductosParaIndice devuelve los productos activos con esos ids, sin receta.
	// Los que no vuelven se dieron de baja.
	ProductosParaIndice(contexto context.Context, ids []string) ([]dominio.Producto, error)
	// ListarParaReindexar devuelve hasta `limite` productos activos con id mayor a
	// `despuesDe`, ordenados por id: sirve para recorrer todo el catálogo en lotes.
	ListarParaReindexar(contexto context.Context, despuesDe string, limite int) ([]dominio.Producto, error)
}

// Buscador es el motor de búsqueda de texto (ADR-008).
type Buscador interface {
	// Buscar devuelve ErrBusquedaNoDisponible si el motor no responde.
	Buscar(contexto context.Context, consulta dominio.Consulta) (dominio.PaginaDeProductos, error)
	// Guardar agrega o reemplaza productos en el índice y espera a que queden indexados.
	Guardar(contexto context.Context, productos []dominio.Producto) error
	// Borrar saca productos del índice y espera a que se apliquen.
	Borrar(contexto context.Context, ids []string) error
	// IniciarReconstruccion abre un índice nuevo, al lado del actual, para cargarlo
	// entero sin dejar la búsqueda vacía mientras tanto.
	IniciarReconstruccion(contexto context.Context) (Reconstruccion, error)
}

// Reconstruccion es un índice nuevo que se carga de a lotes y después reemplaza al actual.
type Reconstruccion interface {
	Agregar(contexto context.Context, productos []dominio.Producto) error
	// Intercambiar deja el índice nuevo como el vigente.
	Intercambiar(contexto context.Context) error
	// Descartar borra el índice nuevo si la reconstrucción no terminó.
	Descartar(contexto context.Context)
}

// CacheDelCatalogo guarda por poco tiempo lo que más se lee del catálogo (ADR-009).
// La aplicación decide cuándo leer, guardar e invalidar; el adaptador decide las
// claves, la vigencia y el formato. Todos los métodos devuelven error si la caché
// no responde, y la aplicación sigue sin ella.
type CacheDelCatalogo interface {
	// Producto devuelve la vista pública del producto y true, o false si no está.
	Producto(contexto context.Context, id string) (dominio.Producto, bool, error)
	GuardarProducto(contexto context.Context, producto dominio.Producto) error
	// Pagina devuelve una página del catálogo y true, o false si no está.
	Pagina(contexto context.Context, filtro dominio.FiltroDeCatalogo) (dominio.PaginaDeProductos, bool, error)
	GuardarPagina(contexto context.Context, filtro dominio.FiltroDeCatalogo, pagina dominio.PaginaDeProductos) error
	// Categorias devuelve la lista de categorías y true, o false si no está.
	Categorias(contexto context.Context) ([]dominio.Categoria, bool, error)
	GuardarCategorias(contexto context.Context, categorias []dominio.Categoria) error
	// InvalidarProducto olvida el detalle de un producto.
	InvalidarProducto(contexto context.Context, id string) error
	// InvalidarCatalogo olvida todas las páginas del catálogo y la lista de categorías.
	InvalidarCatalogo(contexto context.Context) error
}

// AlmacenDeImagenes guarda los archivos de las imágenes y sabe desde qué URL se sirven.
type AlmacenDeImagenes interface {
	// Guardar escribe la imagen con un nombre nuevo y devuelve la URL con la que se sirve.
	Guardar(contexto context.Context, imagen dominio.Imagen) (url string, err error)
	// Borrar elimina la imagen de esa URL; si no existe, no es un error.
	Borrar(contexto context.Context, url string) error
}

// Eventos recibe los hechos que interesan fuera de la aplicación. La aplicación
// solo avisa qué pasó; el adaptador decide qué loguear y qué contar (ADR-006, ADR-007).
type Eventos interface {
	// CacheConsultada avisa si la consulta a la caché acertó. tipo es "producto",
	// "catalogo" o "categorias".
	CacheConsultada(contexto context.Context, tipo string, acierto bool)
	// CacheFallo avisa que la caché no respondió y se siguió sin ella.
	CacheFallo(contexto context.Context, operacion string, err error)
	// IndiceSincronizado avisa cuántos productos se reflejaron en la búsqueda, o por qué no se pudo.
	IndiceSincronizado(contexto context.Context, cantidad int, err error)
	// AntiguedadDelIndice avisa hace cuánto espera el cambio más viejo de la bandeja (0 si está vacía).
	AntiguedadDelIndice(contexto context.Context, antiguedad time.Duration)
	// ImagenSinBorrar avisa que quedó un archivo de imagen que ya no se usa.
	ImagenSinBorrar(contexto context.Context, url string, err error)
}
