package meilisearch

// Tests de integración contra un Meilisearch real. Se saltean si no está MEILI_URL_TEST.
// Para correrlos (Docker en WSL):
//
//	docker run -d --name meili-prueba -p 127.0.0.1:7701:7700 -e MEILI_MASTER_KEY=clave-de-prueba-de-16 getmeili/meilisearch:v1.54.3
//	MEILI_URL_TEST=http://127.0.0.1:7701 MEILI_CLAVE_TEST=clave-de-prueba-de-16 go test ./internal/adaptadores/meilisearch/

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
)

var contexto = context.Background()

func buscadorDePrueba(prueba *testing.T) *Buscador {
	prueba.Helper()
	direccion := os.Getenv("MEILI_URL_TEST")
	if direccion == "" {
		prueba.Skip("falta MEILI_URL_TEST: test de integración salteado")
	}
	buscador := Nuevo(direccion, os.Getenv("MEILI_CLAVE_TEST"))
	buscador.borrarIndice(contexto, indiceVigente)
	if err := buscador.Preparar(contexto); err != nil {
		prueba.Fatal(err)
	}
	return buscador
}

func producto(id, nombre, categoria string, precio int64, celiacos, disponible bool, creado int) dominio.Producto {
	return dominio.Producto{
		ID: id, Nombre: nombre, CategoriaID: "id-" + categoria, Categoria: categoria, PrecioCentavos: precio,
		AptoCeliacos: celiacos, Disponible: disponible, CreadoEn: time.Date(2026, 10, creado, 12, 0, 0, 0, time.UTC),
	}
}

func cargarCatalogo(prueba *testing.T, buscador *Buscador) {
	prueba.Helper()
	err := buscador.Guardar(contexto, []dominio.Producto{
		producto("torta-limon", "Torta de limón", "Tortas", 900000, false, true, 1),
		producto("torta-chocolate", "Torta de chocolate", "Tortas", 700000, true, true, 2),
		producto("torta-boda", "Torta de boda", "Tortas", 5000000, false, false, 3),
		producto("tarta-limon", "Tarta de limón", "Tartas", 450000, false, true, 4),
	})
	if err != nil {
		prueba.Fatal(err)
	}
}

func ids(pagina dominio.PaginaDeProductos) []string {
	var lista []string
	for _, producto := range pagina.Productos {
		lista = append(lista, producto.ID)
	}
	return lista
}

func buscar(prueba *testing.T, buscador *Buscador, consulta dominio.Consulta) dominio.PaginaDeProductos {
	prueba.Helper()
	consulta, err := dominio.NuevaConsulta(consulta)
	if err != nil {
		prueba.Fatal(err)
	}
	pagina, err := buscador.Buscar(contexto, consulta)
	if err != nil {
		prueba.Fatal(err)
	}
	return pagina
}

func TestToleraErroresDeTipeoYTildes(prueba *testing.T) {
	buscador := buscadorDePrueba(prueba)
	cargarCatalogo(prueba, buscador)

	if pagina := buscar(prueba, buscador, dominio.Consulta{Texto: "limon"}); pagina.Total != 2 {
		prueba.Errorf("\"limon\" encuentra las dos de limón: %v", ids(pagina))
	}
	if pagina := buscar(prueba, buscador, dominio.Consulta{Texto: "chocolte"}); len(pagina.Productos) != 1 || pagina.Productos[0].ID != "torta-chocolate" {
		prueba.Errorf("\"chocolte\" encuentra la de chocolate: %v", ids(pagina))
	}
}

// Criterio de aceptación de C3: filtrar por tortas y ordenar por precio ascendente.
func TestFiltraPorCategoriaYOrdenaPorPrecio(prueba *testing.T) {
	buscador := buscadorDePrueba(prueba)
	cargarCatalogo(prueba, buscador)

	disponible := true
	pagina := buscar(prueba, buscador, dominio.Consulta{Categoria: "Tortas", Disponible: &disponible, Orden: dominio.OrdenPrecioAsc})

	if got := ids(pagina); len(got) != 2 || got[0] != "torta-chocolate" || got[1] != "torta-limon" {
		prueba.Errorf("solo tortas disponibles, de la más barata a la más cara: %v", got)
	}
	if conTodas := buscar(prueba, buscador, dominio.Consulta{Categoria: "Tortas", Orden: dominio.OrdenPrecioDesc}); conTodas.Total != 3 || conTodas.Productos[0].ID != "torta-boda" || conTodas.Productos[0].Disponible {
		prueba.Errorf("sin pedir disponibles, vienen todas y cada una dice si se vende: %v", ids(conTodas))
	}
}

func TestOtrosFiltrosYOrden(prueba *testing.T) {
	buscador := buscadorDePrueba(prueba)
	cargarCatalogo(prueba, buscador)
	apto, minimo, maximo := true, int64(500000), int64(950000)

	if pagina := buscar(prueba, buscador, dominio.Consulta{AptoCeliacos: &apto}); pagina.Total != 1 || pagina.Productos[0].ID != "torta-chocolate" {
		prueba.Errorf("apto celíacos: %v", ids(pagina))
	}
	if pagina := buscar(prueba, buscador, dominio.Consulta{PrecioMinimoCentavos: &minimo, PrecioMaximoCentavos: &maximo}); pagina.Total != 2 {
		prueba.Errorf("rango de precio: %v", ids(pagina))
	}
	if pagina := buscar(prueba, buscador, dominio.Consulta{Orden: dominio.OrdenNovedad}); pagina.Productos[0].ID != "tarta-limon" || !pagina.Productos[0].CreadoEn.Equal(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)) {
		prueba.Errorf("por novedad, primero la más nueva: %v", ids(pagina))
	}
	if pagina := buscar(prueba, buscador, dominio.Consulta{Pagina: 2}); len(pagina.Productos) != 0 || pagina.Total != 4 {
		prueba.Errorf("la página 2 está vacía e informa el total: %+v", pagina)
	}
}

func TestUnFiltroConComillasNoSeRompe(prueba *testing.T) {
	buscador := buscadorDePrueba(prueba)
	cargarCatalogo(prueba, buscador)

	pagina := buscar(prueba, buscador, dominio.Consulta{Categoria: `x" OR disponible = false OR categoria = "y`})

	if pagina.Total != 0 {
		prueba.Errorf("las comillas no cambian el filtro: %v", ids(pagina))
	}
}

func TestBorrarYReconstruir(prueba *testing.T) {
	buscador := buscadorDePrueba(prueba)
	cargarCatalogo(prueba, buscador)

	if err := buscador.Borrar(contexto, []string{"torta-limon"}); err != nil {
		prueba.Fatal(err)
	}
	if pagina := buscar(prueba, buscador, dominio.Consulta{Texto: "limon"}); pagina.Total != 1 {
		prueba.Errorf("borrado del índice: %v", ids(pagina))
	}

	reconstruccion, err := buscador.IniciarReconstruccion(contexto)
	if err != nil {
		prueba.Fatal(err)
	}
	if pagina := buscar(prueba, buscador, dominio.Consulta{}); pagina.Total != 3 {
		prueba.Errorf("mientras se reconstruye, la búsqueda sigue con el índice vigente: %v", ids(pagina))
	}
	reconstruccion.Agregar(contexto, []dominio.Producto{producto("pan", "Pan de campo", "Panes", 300000, false, true, 5)})
	if err := reconstruccion.Intercambiar(contexto); err != nil {
		prueba.Fatal(err)
	}
	if pagina := buscar(prueba, buscador, dominio.Consulta{}); pagina.Total != 1 || pagina.Productos[0].ID != "pan" {
		prueba.Errorf("después del intercambio queda solo lo reconstruido: %v", ids(pagina))
	}
	if pagina := buscar(prueba, buscador, dominio.Consulta{Texto: "pan"}); pagina.Total != 1 {
		prueba.Errorf("el índice nuevo tiene la misma configuración: %v", ids(pagina))
	}
}

func TestSinMeilisearchLaBusquedaNoEstaDisponible(prueba *testing.T) {
	buscador := Nuevo("http://127.0.0.1:1", "")

	_, err := buscador.Buscar(contexto, dominio.Consulta{Pagina: 1})

	if !errors.Is(err, puertos.ErrBusquedaNoDisponible) {
		prueba.Errorf("se esperaba ErrBusquedaNoDisponible, dio %v", err)
	}
}
