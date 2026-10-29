package dominio

import (
	"strings"
	"time"
	"unicode/utf8"
)

const maximoNombreCategoria = 50

// Categoria agrupa productos en el catálogo (tortas, tartas, panes...). Las define
// el vendedor.
type Categoria struct {
	ID       string
	Nombre   string
	CreadoEn time.Time
}

// NuevaCategoria arma una categoría con el nombre normalizado.
func NuevaCategoria(id, nombre string, ahora time.Time) (Categoria, error) {
	return Categoria{ID: id, CreadoEn: ahora}.Renombrar(nombre)
}

// Renombrar devuelve la categoría con el nombre nuevo, sin espacios de más:
// "  Tortas  de boda " y "Tortas de boda" son el mismo nombre.
func (categoria Categoria) Renombrar(nombre string) (Categoria, error) {
	nombre = strings.Join(strings.Fields(nombre), " ")
	if nombre == "" || utf8.RuneCountInString(nombre) > maximoNombreCategoria {
		return Categoria{}, invalido("el nombre de la categoría es obligatorio y tiene hasta %d caracteres", maximoNombreCategoria)
	}
	categoria.Nombre = nombre
	return categoria, nil
}
