package aplicacion_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
)

var contexto = context.Background()

const png = "\x89PNG\r\n\x1a\nresto"

// conTorta deja cargada la categoría "Tortas" y una torta disponible.
func conTorta(prueba *testing.T) (*entorno, dominio.Producto) {
	prueba.Helper()
	actual := armar()
	tortas, err := actual.servicio.CrearCategoria(contexto, "Tortas")
	if err != nil {
		prueba.Fatalf("crear categoría: %v", err)
	}
	torta, err := actual.servicio.CrearProducto(contexto, dominio.DatosProducto{Nombre: "Torta de limón", CategoriaID: tortas.ID, PrecioCentavos: 850000, Disponible: true})
	if err != nil {
		prueba.Fatalf("crear producto: %v", err)
	}
	return actual, torta
}

func TestCrearProductoQuedaParaIndexarConSuCategoria(prueba *testing.T) {
	actual, torta := conTorta(prueba)

	if torta.Categoria != "Tortas" {
		prueba.Errorf("el producto creado trae el nombre de su categoría: %+v", torta)
	}
	if guardar, _ := actual.base.bandeja.Resolver(); len(guardar) != 1 || guardar[0] != torta.ID {
		prueba.Errorf("el producto nuevo queda en la bandeja del índice: %v", actual.base.bandeja)
	}
}

func TestCrearProductoConCategoriaInexistente(prueba *testing.T) {
	actual := armar()

	_, err := actual.servicio.CrearProducto(contexto, dominio.DatosProducto{Nombre: "Torta", CategoriaID: "no-existe", PrecioCentavos: 100})

	if !errors.Is(err, dominio.ErrCategoriaNoEncontrada) {
		prueba.Errorf("se esperaba ErrCategoriaNoEncontrada, dio %v", err)
	}
}

func TestDetalleConCache(prueba *testing.T) {
	actual, torta := conTorta(prueba)
	actual.servicio.ReemplazarReceta(contexto, torta.ID, []dominio.Insumo{{Ingrediente: "Harina", Cantidad: 500, Unidad: "g"}})
	actual.base.lecturas = 0

	primero, err := actual.servicio.ObtenerProducto(contexto, torta.ID)
	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	segundo, _ := actual.servicio.ObtenerProducto(contexto, torta.ID)

	if actual.base.lecturas != 1 || actual.eventos.fallos["producto"] != 1 || actual.eventos.aciertos["producto"] != 1 {
		prueba.Errorf("la segunda lectura sale de la caché: lecturas %d, eventos %+v", actual.base.lecturas, actual.eventos)
	}
	if primero.Receta != nil || segundo.Receta != nil || actual.cache.productos[torta.ID].Receta != nil {
		prueba.Error("la vista pública y la caché no llevan la receta")
	}
}

func TestConRedisCaidoSeLeeDeLaBase(prueba *testing.T) {
	actual, torta := conTorta(prueba)
	actual.cache.caida = true

	producto, err := actual.servicio.ObtenerProducto(contexto, torta.ID)

	if err != nil || producto.ID != torta.ID {
		prueba.Fatalf("con Redis caído el detalle sale de la base: %+v %v", producto, err)
	}
	if len(actual.eventos.fallosDeCache) != 1 || actual.eventos.fallosDeCache[0] != "leer_producto" {
		prueba.Errorf("se avisa una sola falla, sin esperar a Redis para guardar: %v", actual.eventos.fallosDeCache)
	}
}

func TestModificarInvalidaElDetalleYElCatalogo(prueba *testing.T) {
	actual, torta := conTorta(prueba)
	actual.servicio.ObtenerProducto(contexto, torta.ID)
	actual.servicio.ListarProductos(contexto, "", false, 1)

	datos := dominio.DatosProducto{Nombre: "Torta de limón", CategoriaID: torta.CategoriaID, PrecioCentavos: 990000, Disponible: true}
	if _, err := actual.servicio.ModificarProducto(contexto, torta.ID, datos); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}

	if _, cacheado := actual.cache.productos[torta.ID]; cacheado || len(actual.cache.paginas) != 0 {
		prueba.Error("modificar borra el detalle y las páginas de la caché")
	}
	detalle, _ := actual.servicio.ObtenerProducto(contexto, torta.ID)
	if detalle.PrecioCentavos != 990000 {
		prueba.Errorf("después de modificar se ve el precio nuevo: %d", detalle.PrecioCentavos)
	}
}

func TestListadoDelVendedorNoUsaLaCache(prueba *testing.T) {
	actual, _ := conTorta(prueba)

	actual.servicio.ListarProductos(contexto, "", true, 1)
	actual.servicio.ListarProductos(contexto, "", true, 1)

	if actual.cache.escrituras != 0 || actual.base.lecturas != 2 {
		prueba.Errorf("el listado con no disponibles va siempre a la base: escrituras %d, lecturas %d", actual.cache.escrituras, actual.base.lecturas)
	}
}

func TestSubirImagenReemplazaLaAnterior(prueba *testing.T) {
	actual, torta := conTorta(prueba)

	primera, err := actual.servicio.SubirImagen(contexto, torta.ID, []byte(png))
	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	actual.servicio.ObtenerProducto(contexto, torta.ID) // queda en la caché con la primera
	segunda, _ := actual.servicio.SubirImagen(contexto, torta.ID, []byte(png))

	if _, sigue := actual.almacen.archivos[primera.ImagenURL]; !sigue {
		prueba.Error("la imagen anterior se conserva: la búsqueda y la caché la pueden seguir mostrando un rato")
	}
	if actual.base.productos[torta.ID].ImagenURL != segunda.ImagenURL {
		prueba.Errorf("el producto queda con la imagen nueva: %+v", actual.base.productos[torta.ID])
	}
	if detalle, _ := actual.servicio.ObtenerProducto(contexto, torta.ID); detalle.ImagenURL != segunda.ImagenURL {
		prueba.Errorf("el detalle ya no sale de la caché vieja: %q", detalle.ImagenURL)
	}
}

func TestImagenInvalidaNoGuardaNada(prueba *testing.T) {
	actual, torta := conTorta(prueba)

	_, err := actual.servicio.SubirImagen(contexto, torta.ID, []byte("no soy una imagen"))

	if !errors.Is(err, dominio.ErrImagenInvalida) || len(actual.almacen.archivos) != 0 {
		prueba.Errorf("se esperaba ErrImagenInvalida sin archivos: %v %v", err, actual.almacen.archivos)
	}
}

func TestSiFallaLaBaseSeBorraLaImagenNueva(prueba *testing.T) {
	actual, torta := conTorta(prueba)
	actual.base.fallarImagen = errors.New("MySQL caído")

	_, err := actual.servicio.SubirImagen(contexto, torta.ID, []byte(png))

	if err == nil || len(actual.almacen.archivos) != 0 {
		prueba.Errorf("sin guardar en la base no queda el archivo suelto: %v %v", err, actual.almacen.archivos)
	}
}

func TestSincronizarIndice(prueba *testing.T) {
	actual, torta := conTorta(prueba)
	actual.ahora = actual.ahora.Add(3 * time.Second)

	cantidad, err := actual.servicio.SincronizarIndice(contexto)

	if err != nil || cantidad != 1 || actual.buscador.indice[torta.ID].Nombre != "Torta de limón" {
		prueba.Fatalf("el producto llega al índice: %d %v %v", cantidad, err, actual.buscador.indice)
	}
	if len(actual.base.bandeja) != 0 || actual.eventos.antiguedad != 3*time.Second {
		prueba.Errorf("la bandeja queda vacía y se avisa la antigüedad: %v %v", actual.base.bandeja, actual.eventos.antiguedad)
	}

	actual.servicio.EliminarProducto(contexto, torta.ID)
	actual.servicio.SincronizarIndice(contexto)
	if _, sigue := actual.buscador.indice[torta.ID]; sigue {
		prueba.Error("un producto dado de baja sale del índice")
	}
}

func TestUnaVueltaVaciaLaBandejaDeALotes(prueba *testing.T) {
	actual := armar()
	for numero := range 250 { // como renombrar una categoría con 250 productos
		actual.base.encolar(fmt.Sprintf("producto-%03d", numero), puertos.IndiceBorrar)
	}

	cantidad, err := actual.servicio.SincronizarIndice(contexto)

	if err != nil || cantidad != 250 || len(actual.base.bandeja) != 0 {
		prueba.Errorf("una sola vuelta procesa los 3 lotes: cantidad %d, quedan %d, error %v", cantidad, len(actual.base.bandeja), err)
	}
}

func TestConElMotorCaidoLosCambiosEsperan(prueba *testing.T) {
	actual, torta := conTorta(prueba)
	actual.buscador.caido = true

	if _, err := actual.servicio.SincronizarIndice(contexto); err == nil || actual.eventos.errIndice == nil {
		prueba.Fatal("con el motor caído la sincronización falla y se avisa")
	}
	if len(actual.base.bandeja) != 1 {
		prueba.Fatalf("la fila queda en la bandeja: %v", actual.base.bandeja)
	}

	actual.buscador.caido = false
	actual.servicio.SincronizarIndice(contexto)
	if _, esta := actual.buscador.indice[torta.ID]; !esta || len(actual.base.bandeja) != 0 {
		prueba.Error("cuando el motor vuelve, el cambio se refleja solo")
	}
}

func TestProductoDadoDeBajaAntesDeIndexarseSeBorra(prueba *testing.T) {
	actual, torta := conTorta(prueba)
	actual.buscador.indice[torta.ID] = torta
	delete(actual.base.productos, torta.ID) // se dio de baja sin que la bandeja tenga todavía el "borrar"

	actual.servicio.SincronizarIndice(contexto)

	if _, sigue := actual.buscador.indice[torta.ID]; sigue {
		prueba.Error("si el producto ya no existe al indexar, se borra del índice")
	}
}

func TestRenombrarCategoriaReindexaSusProductos(prueba *testing.T) {
	actual, torta := conTorta(prueba)
	actual.servicio.SincronizarIndice(contexto)

	if _, err := actual.servicio.RenombrarCategoria(contexto, torta.CategoriaID, "Tortas clásicas"); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	actual.servicio.SincronizarIndice(contexto)

	if actual.buscador.indice[torta.ID].Categoria != "Tortas clásicas" {
		prueba.Errorf("el índice tiene el nombre nuevo de la categoría: %+v", actual.buscador.indice[torta.ID])
	}
}

func TestCategorias(prueba *testing.T) {
	actual, torta := conTorta(prueba)

	if _, err := actual.servicio.CrearCategoria(contexto, "  tortas "); !errors.Is(err, dominio.ErrCategoriaRepetida) {
		prueba.Errorf("se esperaba ErrCategoriaRepetida: %v", err)
	}
	if err := actual.servicio.EliminarCategoria(contexto, torta.CategoriaID); !errors.Is(err, dominio.ErrCategoriaEnUso) {
		prueba.Errorf("se esperaba ErrCategoriaEnUso: %v", err)
	}

	actual.servicio.ListarCategorias(contexto)
	lista, _ := actual.servicio.ListarCategorias(contexto)
	if len(lista) != 1 || actual.eventos.aciertos["categorias"] != 1 {
		prueba.Errorf("la segunda lista sale de la caché: %v %+v", lista, actual.eventos)
	}
	actual.servicio.CrearCategoria(contexto, "Tartas")
	if lista, _ := actual.servicio.ListarCategorias(contexto); len(lista) != 2 {
		prueba.Errorf("crear una categoría invalida la lista: %v", lista)
	}
}

func TestBuscar(prueba *testing.T) {
	actual := armar()

	if _, err := actual.servicio.BuscarProductos(contexto, dominio.Consulta{Texto: " limon ", Orden: dominio.OrdenPrecioAsc}); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if actual.buscador.consulta.Pagina != 1 || actual.buscador.consulta.Texto != "limon" {
		prueba.Errorf("al buscador llega la consulta completa: %+v", actual.buscador.consulta)
	}
	if _, err := actual.servicio.BuscarProductos(contexto, dominio.Consulta{Orden: "cualquiera"}); !errors.Is(err, dominio.ErrSolicitudInvalida) {
		prueba.Errorf("una consulta inválida no llega al buscador: %v", err)
	}

	actual.buscador.caido = true
	if _, err := actual.servicio.BuscarProductos(contexto, dominio.Consulta{}); !errors.Is(err, puertos.ErrBusquedaNoDisponible) {
		prueba.Errorf("se esperaba ErrBusquedaNoDisponible: %v", err)
	}
}

func TestReindexar(prueba *testing.T) {
	actual, torta := conTorta(prueba)
	actual.buscador.indice["fantasma"] = dominio.Producto{ID: "fantasma"}

	total, err := actual.servicio.Reindexar(contexto)

	if err != nil || total != 1 || !actual.buscador.intercambiado {
		prueba.Fatalf("total %d, error %v", total, err)
	}
	if _, esta := actual.buscador.indice[torta.ID]; !esta || len(actual.buscador.indice) != 1 {
		prueba.Errorf("el índice nuevo tiene exactamente lo que hay en la base: %v", actual.buscador.indice)
	}
}

func TestReindexarQueFallaDescartaElIndiceNuevo(prueba *testing.T) {
	actual, _ := conTorta(prueba)
	actual.buscador.fallarAlAgregar = true

	if _, err := actual.servicio.Reindexar(contexto); err == nil || !actual.buscador.descartado || actual.buscador.intercambiado {
		prueba.Errorf("si falla, se descarta y el índice vigente no cambia: %v", err)
	}
}
