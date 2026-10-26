package dominio_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
)

var ahora = time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)

func datosValidos() dominio.DatosProducto {
	return dominio.DatosProducto{Nombre: "  Torta de limón ", CategoriaID: "categoria-1", PrecioCentavos: 850000, AnticipacionMinimaDias: 2, Disponible: true}
}

func productoValido(prueba *testing.T) dominio.Producto {
	prueba.Helper()
	producto, err := dominio.NuevoProducto("producto-1", datosValidos(), ahora)
	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	return producto
}

func TestNuevoProductoNormalizaLosDatos(prueba *testing.T) {
	producto := productoValido(prueba)

	if producto.Nombre != "Torta de limón" || producto.CreadoEn != ahora || producto.ActualizadoEn != ahora || producto.ID != "producto-1" {
		prueba.Errorf("producto = %+v", producto)
	}
}

func TestDatosInvalidos(prueba *testing.T) {
	casos := map[string]func(*dominio.DatosProducto){
		"sin nombre":            func(datos *dominio.DatosProducto) { datos.Nombre = "   " },
		"nombre larguísimo":     func(datos *dominio.DatosProducto) { datos.Nombre = strings.Repeat("a", 101) },
		"sin categoría":         func(datos *dominio.DatosProducto) { datos.CategoriaID = "" },
		"precio cero":           func(datos *dominio.DatosProducto) { datos.PrecioCentavos = 0 },
		"precio negativo":       func(datos *dominio.DatosProducto) { datos.PrecioCentavos = -1 },
		"precio desmedido":      func(datos *dominio.DatosProducto) { datos.PrecioCentavos = 100_000_001 },
		"anticipación negativa": func(datos *dominio.DatosProducto) { datos.AnticipacionMinimaDias = -1 },
		"anticipación enorme":   func(datos *dominio.DatosProducto) { datos.AnticipacionMinimaDias = 61 },
		"descripción enorme":    func(datos *dominio.DatosProducto) { datos.Descripcion = strings.Repeat("a", 1001) },
	}
	for nombre, estropear := range casos {
		prueba.Run(nombre, func(prueba *testing.T) {
			datos := datosValidos()
			estropear(&datos)
			if _, err := dominio.NuevoProducto("producto-1", datos, ahora); !errors.Is(err, dominio.ErrSolicitudInvalida) {
				prueba.Errorf("se esperaba ErrSolicitudInvalida, dio %v", err)
			}
		})
	}
}

func TestModificarConservaLaImagenYLaReceta(prueba *testing.T) {
	original, _ := productoValido(prueba).CambiarImagen("/imagenes/torta.png", ahora)
	original, _ = original.ReemplazarReceta([]dominio.Insumo{{Ingrediente: "Harina", Cantidad: 500, Unidad: dominio.UnidadGramos}}, ahora)
	original.Categoria = "Tortas"
	datos := datosValidos()
	datos.PrecioCentavos = 900000
	datos.CategoriaID = "categoria-2"

	modificado, err := original.Modificar(datos, ahora.Add(time.Hour))

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if modificado.PrecioCentavos != 900000 || modificado.ImagenURL != "/imagenes/torta.png" || len(modificado.Receta) != 1 || modificado.CreadoEn != ahora {
		prueba.Errorf("modificado = %+v", modificado)
	}
	if modificado.Categoria != "" || original.Categoria != "Tortas" || original.PrecioCentavos != 850000 {
		prueba.Errorf("el nombre de la categoría vieja se borra y el original no cambia: %+v / %+v", modificado, original)
	}
}

func TestReemplazarReceta(prueba *testing.T) {
	despues := ahora.Add(time.Hour)
	producto, err := productoValido(prueba).ReemplazarReceta([]dominio.Insumo{
		{Ingrediente: " Harina ", Cantidad: 500, Unidad: "g"},
		{Ingrediente: "Harina", Cantidad: 5, Unidad: "ml"}, // otra unidad: es válido
		{Ingrediente: "Huevo", Cantidad: 3, Unidad: "unidad"},
	}, despues)
	if err != nil || producto.Receta[0].Ingrediente != "Harina" || len(producto.Receta) != 3 || producto.ActualizadoEn != despues {
		prueba.Fatalf("receta válida: %+v %v", producto, err)
	}

	malas := map[string][]dominio.Insumo{
		"cantidad cero":      {{Ingrediente: "Harina", Cantidad: 0, Unidad: "g"}},
		"unidad desconocida": {{Ingrediente: "Harina", Cantidad: 5, Unidad: "kg"}},
		"sin ingrediente":    {{Ingrediente: " ", Cantidad: 5, Unidad: "g"}},
		"repetido":           {{Ingrediente: "Harina", Cantidad: 5, Unidad: "g"}, {Ingrediente: "harina", Cantidad: 5, Unidad: "g"}},
	}
	for nombre, receta := range malas {
		if _, err := productoValido(prueba).ReemplazarReceta(receta, ahora); !errors.Is(err, dominio.ErrSolicitudInvalida) {
			prueba.Errorf("%s: se esperaba ErrSolicitudInvalida, dio %v", nombre, err)
		}
	}
}

func TestCambiarImagenDevuelveLaAnterior(prueba *testing.T) {
	primera, anterior := productoValido(prueba).CambiarImagen("/imagenes/a.png", ahora)
	if anterior != "" {
		prueba.Errorf("sin imagen previa, la anterior es vacía: %q", anterior)
	}

	segunda, anterior := primera.CambiarImagen("/imagenes/b.png", ahora)

	if anterior != "/imagenes/a.png" || segunda.ImagenURL != "/imagenes/b.png" {
		prueba.Errorf("anterior %q, actual %q", anterior, segunda.ImagenURL)
	}
}

func TestVistaPublicaSinReceta(prueba *testing.T) {
	producto, _ := productoValido(prueba).ReemplazarReceta([]dominio.Insumo{{Ingrediente: "Harina", Cantidad: 5, Unidad: "g"}}, ahora)

	if publica := producto.VistaPublica(); publica.Receta != nil || len(producto.Receta) != 1 {
		prueba.Errorf("la vista pública no lleva receta y el original la conserva: %+v / %+v", publica, producto)
	}
}

func TestCategoria(prueba *testing.T) {
	categoria, err := dominio.NuevaCategoria("categoria-1", "  Tortas   de  boda ", ahora)
	if err != nil || categoria.Nombre != "Tortas de boda" || categoria.ID != "categoria-1" {
		prueba.Fatalf("categoría = %+v, %v", categoria, err)
	}
	if _, err := categoria.Renombrar("   "); !errors.Is(err, dominio.ErrSolicitudInvalida) {
		prueba.Errorf("un nombre vacío es inválido: %v", err)
	}
	if _, err := categoria.Renombrar(strings.Repeat("a", 51)); !errors.Is(err, dominio.ErrSolicitudInvalida) {
		prueba.Errorf("un nombre de 51 caracteres es inválido: %v", err)
	}
}

func TestNuevaImagenMiraElContenido(prueba *testing.T) {
	casos := map[string]struct {
		contenido []byte
		extension string
	}{
		"png":  {[]byte("\x89PNG\r\n\x1a\nresto"), ".png"},
		"jpg":  {[]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}, ".jpg"},
		"webp": {[]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), ".webp"},
	}
	for nombre, caso := range casos {
		imagen, err := dominio.NuevaImagen(caso.contenido)
		if err != nil || imagen.Extension != caso.extension {
			prueba.Errorf("%s: %+v %v", nombre, imagen.Extension, err)
		}
	}

	invalidas := map[string][]byte{
		"vacía":             nil,
		"texto con nombre":  []byte("esto no es una imagen"),
		"gif":               []byte("GIF89a"),
		"demasiado pesada":  append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, dominio.MaximoBytesImagen)...),
		"riff que no es wp": []byte("RIFF\x00\x00\x00\x00WAVEfmt "),
	}
	for nombre, contenido := range invalidas {
		if _, err := dominio.NuevaImagen(contenido); !errors.Is(err, dominio.ErrImagenInvalida) {
			prueba.Errorf("%s: se esperaba ErrImagenInvalida, dio %v", nombre, err)
		}
	}
}

func TestNuevaConsulta(prueba *testing.T) {
	porDefecto, err := dominio.NuevaConsulta(dominio.Consulta{Texto: "  limon "})
	if err != nil || porDefecto.Pagina != 1 || porDefecto.Texto != "limon" || porDefecto.Orden != dominio.OrdenRelevancia {
		prueba.Errorf("valores por defecto: %+v %v", porDefecto, err)
	}

	menos, mas := int64(100), int64(50)
	casos := map[string]dominio.Consulta{
		"página negativa":   {Pagina: -1},
		"página enorme":     {Pagina: 1001},
		"orden desconocido": {Orden: "alfabetico"},
		"rango invertido":   {PrecioMinimoCentavos: &menos, PrecioMaximoCentavos: &mas},
		"texto enorme":      {Texto: strings.Repeat("a", 101)},
		"mínimo negativo":   {PrecioMinimoCentavos: ptr(int64(-1))},
	}
	for nombre, consulta := range casos {
		if _, err := dominio.NuevaConsulta(consulta); !errors.Is(err, dominio.ErrSolicitudInvalida) {
			prueba.Errorf("%s: se esperaba ErrSolicitudInvalida, dio %v", nombre, err)
		}
	}
}

func TestFiltroDeCatalogo(prueba *testing.T) {
	filtro, err := dominio.NuevoFiltroDeCatalogo("", false, 0)
	if err != nil || filtro.Pagina != 1 || !filtro.SeCachea() {
		prueba.Errorf("filtro del comprador: %+v %v", filtro, err)
	}
	if filtro, _ := dominio.NuevoFiltroDeCatalogo("", true, 2); filtro.SeCachea() {
		prueba.Error("el listado del vendedor, con no disponibles, no se cachea")
	}
	if _, err := dominio.NuevoFiltroDeCatalogo("", false, -3); !errors.Is(err, dominio.ErrSolicitudInvalida) {
		prueba.Errorf("página negativa: %v", err)
	}
}

func ptr[Tipo any](valor Tipo) *Tipo { return &valor }
