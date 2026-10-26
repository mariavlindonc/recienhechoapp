package redis

// Tests de integración contra un Redis real. Se saltean si no está REDIS_URL_TEST.
// Para correrlos (Docker en WSL):
//
//	docker run -d --name redis-prueba -p 127.0.0.1:6380:6379 redis:7
//	REDIS_URL_TEST=redis://127.0.0.1:6380/0 go test ./internal/adaptadores/redis/

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
)

var contexto = context.Background()

func cacheDePrueba(prueba *testing.T) *Cache {
	prueba.Helper()
	direccion := os.Getenv("REDIS_URL_TEST")
	if direccion == "" {
		prueba.Skip("falta REDIS_URL_TEST: test de integración salteado")
	}
	cache, err := Nueva(direccion)
	if err != nil {
		prueba.Fatal(err)
	}
	if err := cache.cliente.FlushDB(contexto).Err(); err != nil {
		prueba.Fatal(err)
	}
	prueba.Cleanup(func() { cache.Cerrar() })
	return cache
}

func TestProductoIdaYVueltaEInvalidacion(prueba *testing.T) {
	cache := cacheDePrueba(prueba)
	torta := dominio.Producto{ID: "torta", Nombre: "Torta de limón", PrecioCentavos: 850000, CreadoEn: time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)}

	if _, encontrado, err := cache.Producto(contexto, "torta"); encontrado || err != nil {
		prueba.Fatalf("vacía: %v %v", encontrado, err)
	}
	if err := cache.GuardarProducto(contexto, torta); err != nil {
		prueba.Fatal(err)
	}
	leido, encontrado, err := cache.Producto(contexto, "torta")
	if !encontrado || err != nil || leido.Nombre != torta.Nombre || !leido.CreadoEn.Equal(torta.CreadoEn) {
		prueba.Fatalf("leído = %+v %v %v", leido, encontrado, err)
	}
	if vigencia := cache.cliente.TTL(contexto, "producto:torta").Val(); vigencia <= 0 || vigencia > 60*time.Second {
		prueba.Errorf("la clave vence a los 60 s: %v", vigencia)
	}

	cache.InvalidarProducto(contexto, "torta")
	if _, encontrado, _ := cache.Producto(contexto, "torta"); encontrado {
		prueba.Error("invalidar borra el detalle")
	}
}

func TestInvalidarCatalogoCambiaDeVersion(prueba *testing.T) {
	cache := cacheDePrueba(prueba)
	filtro := dominio.FiltroDeCatalogo{CategoriaID: "tortas", Pagina: 1}
	cache.GuardarPagina(contexto, filtro, dominio.PaginaDeProductos{Pagina: 1, Total: 3})
	cache.GuardarCategorias(contexto, []dominio.Categoria{{ID: "tortas", Nombre: "Tortas"}})

	if pagina, encontrada, _ := cache.Pagina(contexto, filtro); !encontrada || pagina.Total != 3 {
		prueba.Fatalf("página guardada: %+v %v", pagina, encontrada)
	}
	if _, encontrada, _ := cache.Pagina(contexto, dominio.FiltroDeCatalogo{CategoriaID: "tortas", Pagina: 2}); encontrada {
		prueba.Error("otra página es otra clave")
	}

	if err := cache.InvalidarCatalogo(contexto); err != nil {
		prueba.Fatal(err)
	}

	if _, encontrada, _ := cache.Pagina(contexto, filtro); encontrada {
		prueba.Error("después de invalidar, la página vieja no se lee")
	}
	if _, encontradas, _ := cache.Categorias(contexto); encontradas {
		prueba.Error("después de invalidar, las categorías viejas no se leen")
	}
	if vigencia := cache.cliente.TTL(contexto, claveVersion).Val(); vigencia != -1 {
		prueba.Errorf("la versión del catálogo no vence: %v", vigencia)
	}
}

func TestRedisCaidoDevuelveErrorRapido(prueba *testing.T) {
	cache, _ := Nueva("redis://127.0.0.1:1/0") // nadie escucha en el puerto 1
	defer cache.Cerrar()

	inicio := time.Now()
	_, _, err := cache.Producto(contexto, "torta")

	if err == nil {
		prueba.Fatal("sin Redis tiene que devolver error")
	}
	if demora := time.Since(inicio); demora > 500*time.Millisecond {
		prueba.Errorf("sin Redis, el error llega enseguida: tardó %v", demora)
	}
}

func TestApagadaNuncaEncuentra(prueba *testing.T) {
	var cache Apagada
	cache.GuardarProducto(contexto, dominio.Producto{ID: "torta"})

	if _, encontrado, err := cache.Producto(contexto, "torta"); encontrado || err != nil {
		prueba.Errorf("apagada: %v %v", encontrado, err)
	}
}
