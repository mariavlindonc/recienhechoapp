// Package redis implementa la caché del catálogo con Redis (ADR-009): las claves,
// la versión del catálogo, la vigencia y el formato se deciden acá; la aplicación
// solo pide, guarda e invalida.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
)

const (
	// vigencia de todas las claves menos la versión del catálogo, que no vence:
	// si se reiniciara, podrían volver a leerse páginas viejas.
	vigencia = 60 * time.Second
	// tiempoMaximo por operación: si Redis tarda más, se sigue con MySQL (ADR-009).
	tiempoMaximo = 50 * time.Millisecond

	claveVersion = "catalogo:version"
)

// Cache cumple puertos.CacheDelCatalogo.
type Cache struct {
	cliente *goredis.Client
}

var _ puertos.CacheDelCatalogo = (*Cache)(nil)

// Nueva se conecta a la URL dada (redis://host:6379/0). No falla si Redis no está:
// la caché no es obligatoria y cada operación devuelve su error.
func Nueva(direccion string) (*Cache, error) {
	opciones, err := goredis.ParseURL(direccion)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL inválida: %w", err)
	}
	opciones.DialTimeout = tiempoMaximo
	opciones.ReadTimeout = tiempoMaximo
	opciones.WriteTimeout = tiempoMaximo
	opciones.MaxRetries = -1 // sin reintentos: un reintento haría esperar más que los 50 ms
	return &Cache{cliente: goredis.NewClient(opciones)}, nil
}

// Cerrar libera las conexiones.
func (cache *Cache) Cerrar() error { return cache.cliente.Close() }

func claveProducto(id string) string { return "producto:" + id }

func (cache *Cache) Producto(contexto context.Context, id string) (dominio.Producto, bool, error) {
	var producto dominio.Producto
	encontrado, err := cache.leer(contexto, claveProducto(id), &producto)
	return producto, encontrado, err
}

func (cache *Cache) GuardarProducto(contexto context.Context, producto dominio.Producto) error {
	return cache.guardar(contexto, claveProducto(producto.ID), producto)
}

func (cache *Cache) InvalidarProducto(contexto context.Context, id string) error {
	contexto, cancelar := context.WithTimeout(contexto, tiempoMaximo)
	defer cancelar()
	return cache.cliente.Del(contexto, claveProducto(id)).Err()
}

// Las páginas y las categorías llevan la versión del catálogo en la clave. Invalidar
// es subir la versión: las claves viejas dejan de leerse y vencen solas, sin
// buscarlas una por una.
//
// shortcut: si se invalida entre que se lee la base y se guarda la página, se
// guarda una página vieja con la versión nueva. Dura hasta la vigencia (60 s);
// pasar la versión leída al guardar lo evita, si se nota.

func (cache *Cache) Pagina(contexto context.Context, filtro dominio.FiltroDeCatalogo) (dominio.PaginaDeProductos, bool, error) {
	var pagina dominio.PaginaDeProductos
	clave, err := cache.claveDePagina(contexto, filtro)
	if err != nil {
		return pagina, false, err
	}
	encontrado, err := cache.leer(contexto, clave, &pagina)
	return pagina, encontrado, err
}

func (cache *Cache) GuardarPagina(contexto context.Context, filtro dominio.FiltroDeCatalogo, pagina dominio.PaginaDeProductos) error {
	clave, err := cache.claveDePagina(contexto, filtro)
	if err != nil {
		return err
	}
	return cache.guardar(contexto, clave, pagina)
}

func (cache *Cache) Categorias(contexto context.Context) ([]dominio.Categoria, bool, error) {
	var categorias []dominio.Categoria
	clave, err := cache.claveDeCategorias(contexto)
	if err != nil {
		return nil, false, err
	}
	encontrado, err := cache.leer(contexto, clave, &categorias)
	return categorias, encontrado, err
}

func (cache *Cache) GuardarCategorias(contexto context.Context, categorias []dominio.Categoria) error {
	clave, err := cache.claveDeCategorias(contexto)
	if err != nil {
		return err
	}
	return cache.guardar(contexto, clave, categorias)
}

func (cache *Cache) InvalidarCatalogo(contexto context.Context) error {
	contexto, cancelar := context.WithTimeout(contexto, tiempoMaximo)
	defer cancelar()
	return cache.cliente.Incr(contexto, claveVersion).Err()
}

func (cache *Cache) version(contexto context.Context) (int64, error) {
	contexto, cancelar := context.WithTimeout(contexto, tiempoMaximo)
	defer cancelar()
	version, err := cache.cliente.Get(contexto, claveVersion).Int64()
	if errors.Is(err, goredis.Nil) {
		return 0, nil
	}
	return version, err
}

func (cache *Cache) claveDePagina(contexto context.Context, filtro dominio.FiltroDeCatalogo) (string, error) {
	version, err := cache.version(contexto)
	if err != nil {
		return "", err
	}
	categoria := filtro.CategoriaID
	if categoria == "" {
		categoria = "todas"
	}
	return "catalogo:v" + strconv.FormatInt(version, 10) + ":" + categoria + ":" + strconv.Itoa(filtro.Pagina), nil
}

func (cache *Cache) claveDeCategorias(contexto context.Context) (string, error) {
	version, err := cache.version(contexto)
	if err != nil {
		return "", err
	}
	return "categorias:v" + strconv.FormatInt(version, 10), nil
}

func (cache *Cache) leer(contexto context.Context, clave string, destino any) (bool, error) {
	contexto, cancelar := context.WithTimeout(contexto, tiempoMaximo)
	defer cancelar()
	contenido, err := cache.cliente.Get(contexto, clave).Bytes()
	if errors.Is(err, goredis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(contenido, destino); err != nil {
		// Un valor que no se entiende (por ejemplo, de una versión anterior del
		// servicio) cuenta como que no está: se vuelve a leer de la base.
		return false, nil
	}
	return true, nil
}

func (cache *Cache) guardar(contexto context.Context, clave string, valor any) error {
	contenido, err := json.Marshal(valor)
	if err != nil {
		return fmt.Errorf("convertir a JSON para la caché: %w", err)
	}
	contexto, cancelar := context.WithTimeout(contexto, tiempoMaximo)
	defer cancelar()
	return cache.cliente.Set(contexto, clave, contenido, vigencia).Err()
}

// Apagada es la caché cuando CACHE_HABILITADA=false: nunca encuentra nada y no
// guarda nada. Sirve para medir el catálogo sin caché (ADR-009).
type Apagada struct{}

var _ puertos.CacheDelCatalogo = Apagada{}

func (Apagada) Producto(context.Context, string) (dominio.Producto, bool, error) {
	return dominio.Producto{}, false, nil
}
func (Apagada) GuardarProducto(context.Context, dominio.Producto) error { return nil }
func (Apagada) Pagina(context.Context, dominio.FiltroDeCatalogo) (dominio.PaginaDeProductos, bool, error) {
	return dominio.PaginaDeProductos{}, false, nil
}
func (Apagada) GuardarPagina(context.Context, dominio.FiltroDeCatalogo, dominio.PaginaDeProductos) error {
	return nil
}
func (Apagada) Categorias(context.Context) ([]dominio.Categoria, bool, error) {
	return nil, false, nil
}
func (Apagada) GuardarCategorias(context.Context, []dominio.Categoria) error { return nil }
func (Apagada) InvalidarProducto(context.Context, string) error              { return nil }
func (Apagada) InvalidarCatalogo(context.Context) error                      { return nil }
