// Package dominio tiene las entidades y las reglas de productos: el catálogo, sus
// categorías y el recetario (C2, C3, VE1, VE2). No conoce HTTP, bases de datos,
// caché ni el motor de búsqueda (ADR-007).
//
// Las entidades deciden sus propios cambios (tell, don't ask): la aplicación les
// ordena qué hacer y ellas validan y devuelven el resultado.
package dominio

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Unidades en las que se miden los insumos de una receta.
const (
	UnidadGramos     = "g"
	UnidadMililitros = "ml"
	UnidadUnidades   = "unidad"
)

// Límites de los datos. Los montos van siempre en centavos (R5).
const (
	maximoNombre           = 100
	maximoDescripcion      = 1000
	maximoPrecioCentavos   = 100_000_000
	maximoAnticipacionDias = 60
	maximoInsumos          = 50
	maximoIngrediente      = 100
	maximaCantidadInsumo   = 1_000_000
)

// Insumo es un ingrediente de la receta de un producto, por unidad de producto.
type Insumo struct {
	Ingrediente string
	Cantidad    int64
	Unidad      string
}

// Producto es lo que se vende. Receta solo viene cargada cuando la pide el
// vendedor o pedidos; la vista pública no la muestra.
type Producto struct {
	ID                     string
	Nombre                 string
	Descripcion            string
	CategoriaID            string
	Categoria              string // nombre, para mostrar y buscar; lo completa el repositorio
	PrecioCentavos         int64
	AnticipacionMinimaDias int
	AptoCeliacos           bool
	Disponible             bool
	ImagenURL              string
	Receta                 []Insumo
	CreadoEn               time.Time
	ActualizadoEn          time.Time
}

// DatosProducto es lo que el vendedor completa al dar de alta o modificar un
// producto (VE1). La receta y la imagen se cargan aparte.
type DatosProducto struct {
	Nombre                 string
	Descripcion            string
	CategoriaID            string
	PrecioCentavos         int64
	AnticipacionMinimaDias int
	AptoCeliacos           bool
	Disponible             bool
}

func invalido(formato string, argumentos ...any) error {
	return fmt.Errorf("%w: %s", ErrSolicitudInvalida, fmt.Sprintf(formato, argumentos...))
}

// NuevoProducto valida los datos y arma el producto, todavía sin imagen ni receta.
func NuevoProducto(id string, datos DatosProducto, ahora time.Time) (Producto, error) {
	producto, err := Producto{ID: id, CreadoEn: ahora}.Modificar(datos, ahora)
	if err != nil {
		return Producto{}, err
	}
	return producto, nil
}

// Modificar devuelve el producto con los datos nuevos, sin tocar su imagen ni su
// receta. El producto original no cambia.
func (producto Producto) Modificar(datos DatosProducto, ahora time.Time) (Producto, error) {
	datos.Nombre = strings.TrimSpace(datos.Nombre)
	datos.Descripcion = strings.TrimSpace(datos.Descripcion)
	switch {
	case datos.Nombre == "" || utf8.RuneCountInString(datos.Nombre) > maximoNombre:
		return Producto{}, invalido("el nombre es obligatorio y tiene hasta %d caracteres", maximoNombre)
	case utf8.RuneCountInString(datos.Descripcion) > maximoDescripcion:
		return Producto{}, invalido("la descripción tiene hasta %d caracteres", maximoDescripcion)
	case datos.CategoriaID == "":
		return Producto{}, invalido("la categoría es obligatoria")
	case datos.PrecioCentavos <= 0 || datos.PrecioCentavos > maximoPrecioCentavos:
		return Producto{}, invalido("el precio va de 1 a %d centavos", maximoPrecioCentavos)
	case datos.AnticipacionMinimaDias < 0 || datos.AnticipacionMinimaDias > maximoAnticipacionDias:
		return Producto{}, invalido("la anticipación mínima va de 0 a %d días", maximoAnticipacionDias)
	}
	if producto.CategoriaID != datos.CategoriaID {
		producto.Categoria = "" // el nombre de la categoría nueva lo completa el repositorio
	}
	producto.Nombre = datos.Nombre
	producto.Descripcion = datos.Descripcion
	producto.CategoriaID = datos.CategoriaID
	producto.PrecioCentavos = datos.PrecioCentavos
	producto.AnticipacionMinimaDias = datos.AnticipacionMinimaDias
	producto.AptoCeliacos = datos.AptoCeliacos
	producto.Disponible = datos.Disponible
	producto.ActualizadoEn = ahora
	return producto, nil
}

// ReemplazarReceta valida la receta nueva (VE2) y la deja en lugar de la
// anterior. Un mismo ingrediente no puede repetirse en la misma unidad.
func (producto Producto) ReemplazarReceta(receta []Insumo, ahora time.Time) (Producto, error) {
	if len(receta) > maximoInsumos {
		return Producto{}, invalido("la receta tiene hasta %d insumos", maximoInsumos)
	}
	vistos := make(map[string]bool, len(receta))
	limpia := make([]Insumo, 0, len(receta))
	for _, insumo := range receta {
		insumo.Ingrediente = strings.TrimSpace(insumo.Ingrediente)
		switch {
		case insumo.Ingrediente == "" || utf8.RuneCountInString(insumo.Ingrediente) > maximoIngrediente:
			return Producto{}, invalido("el ingrediente es obligatorio y tiene hasta %d caracteres", maximoIngrediente)
		case insumo.Cantidad <= 0 || insumo.Cantidad > maximaCantidadInsumo:
			return Producto{}, invalido("la cantidad de %s va de 1 a %d", insumo.Ingrediente, maximaCantidadInsumo)
		case insumo.Unidad != UnidadGramos && insumo.Unidad != UnidadMililitros && insumo.Unidad != UnidadUnidades:
			return Producto{}, invalido("la unidad de %s tiene que ser %s, %s o %s", insumo.Ingrediente, UnidadGramos, UnidadMililitros, UnidadUnidades)
		}
		clave := strings.ToLower(insumo.Ingrediente) + "|" + insumo.Unidad
		if vistos[clave] {
			return Producto{}, invalido("%s está repetido en %s", insumo.Ingrediente, insumo.Unidad)
		}
		vistos[clave] = true
		limpia = append(limpia, insumo)
	}
	producto.Receta = limpia
	producto.ActualizadoEn = ahora
	return producto, nil
}

// CambiarImagen deja la URL de la foto nueva.
func (producto Producto) CambiarImagen(url string, ahora time.Time) Producto {
	producto.ImagenURL = url
	producto.ActualizadoEn = ahora
	return producto
}

// VistaPublica es el producto tal como lo ve el comprador: sin la receta.
func (producto Producto) VistaPublica() Producto {
	producto.Receta = nil
	return producto
}
