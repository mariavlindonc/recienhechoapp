package dominio

import (
	"strings"
	"unicode/utf8"
)

// TamanioPagina es la cantidad de productos por página (criterio de C3).
const TamanioPagina = 20

const (
	maximoTextoDeBusqueda = 100
	maximaPagina          = 1000
)

// Orden de los resultados de una búsqueda.
type Orden string

const (
	OrdenRelevancia Orden = ""
	OrdenPrecioAsc  Orden = "precio_asc"
	OrdenPrecioDesc Orden = "precio_desc"
	OrdenNovedad    Orden = "novedad"
)

// Consulta es una búsqueda de productos con filtros, orden y paginación (C3).
// Los filtros vacíos no filtran.
type Consulta struct {
	Texto                string
	CategoriaID          string
	PrecioMinimoCentavos *int64
	PrecioMaximoCentavos *int64
	AptoCeliacos         *bool
	// IncluirNoDisponibles es para el vendedor: el comprador solo ve lo que se vende.
	IncluirNoDisponibles bool
	Orden                Orden
	Pagina               int // desde 1
}

// NuevaConsulta completa los valores por defecto (página 1, orden por relevancia)
// y valida que la consulta tenga sentido antes de mandarla al buscador.
func NuevaConsulta(pedida Consulta) (Consulta, error) {
	pedida.Texto = strings.TrimSpace(pedida.Texto)
	pagina, err := normalizarPagina(pedida.Pagina)
	if err != nil {
		return Consulta{}, err
	}
	pedida.Pagina = pagina
	switch {
	case utf8.RuneCountInString(pedida.Texto) > maximoTextoDeBusqueda:
		return Consulta{}, invalido("el texto de búsqueda tiene hasta %d caracteres", maximoTextoDeBusqueda)
	case pedida.Orden != OrdenRelevancia && pedida.Orden != OrdenPrecioAsc && pedida.Orden != OrdenPrecioDesc && pedida.Orden != OrdenNovedad:
		return Consulta{}, invalido("orden desconocido: %q", pedida.Orden)
	case pedida.PrecioMinimoCentavos != nil && *pedida.PrecioMinimoCentavos < 0:
		return Consulta{}, invalido("el precio mínimo no puede ser negativo")
	case pedida.PrecioMaximoCentavos != nil && *pedida.PrecioMaximoCentavos < 0:
		return Consulta{}, invalido("el precio máximo no puede ser negativo")
	case pedida.PrecioMinimoCentavos != nil && pedida.PrecioMaximoCentavos != nil && *pedida.PrecioMinimoCentavos > *pedida.PrecioMaximoCentavos:
		return Consulta{}, invalido("el precio mínimo no puede superar al máximo")
	}
	return pedida, nil
}

// FiltroDeCatalogo es un pedido de una página del catálogo (C2): sin texto, por
// categoría y ordenado por nombre. Lo resuelve la base, con caché.
type FiltroDeCatalogo struct {
	CategoriaID          string // vacío: todas
	IncluirNoDisponibles bool   // solo el vendedor
	Pagina               int
}

// NuevoFiltroDeCatalogo completa la página por defecto y la valida.
func NuevoFiltroDeCatalogo(categoriaID string, incluirNoDisponibles bool, pagina int) (FiltroDeCatalogo, error) {
	pagina, err := normalizarPagina(pagina)
	if err != nil {
		return FiltroDeCatalogo{}, err
	}
	return FiltroDeCatalogo{CategoriaID: categoriaID, IncluirNoDisponibles: incluirNoDisponibles, Pagina: pagina}, nil
}

// SeCachea dice si la página es la misma para todos los compradores. La del
// vendedor, con los productos no disponibles, se lee siempre de la base.
func (filtro FiltroDeCatalogo) SeCachea() bool {
	return !filtro.IncluirNoDisponibles
}

// normalizarPagina toma 0 como "sin página pedida" y la lleva a 1.
func normalizarPagina(pagina int) (int, error) {
	if pagina == 0 {
		pagina = 1
	}
	if pagina < 1 || pagina > maximaPagina {
		return 0, invalido("la página va de 1 a %d", maximaPagina)
	}
	return pagina, nil
}

// PaginaDeProductos es una página de resultados, del catálogo o de una búsqueda.
type PaginaDeProductos struct {
	Productos []Producto
	Pagina    int
	Total     int // cantidad de productos que cumplen el filtro, en todas las páginas
}
