package mysql

// Tests de integración contra un MySQL real. Se saltean si no está MYSQL_DSN_TEST.
// Para correrlos (Docker en WSL):
//
//	docker run -d --name mysql-prueba -p 127.0.0.1:3307:3306 -e MYSQL_ROOT_PASSWORD=prueba -e MYSQL_DATABASE=productos_test mysql:8.4
//	MYSQL_DSN_TEST="root:prueba@tcp(127.0.0.1:3307)/productos_test" go test ./internal/adaptadores/mysql/
//
// Cada test borra las tablas y las vuelve a crear.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
)

var (
	contexto = context.Background()
	ahora    = time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)
)

func repositorioDePrueba(prueba *testing.T) *Repositorio {
	prueba.Helper()
	dsn := os.Getenv("MYSQL_DSN_TEST")
	if dsn == "" {
		prueba.Skip("falta MYSQL_DSN_TEST: test de integración salteado")
	}
	base, err := Abrir(dsn)
	if err != nil {
		prueba.Fatal(err)
	}
	if err := base.Migrator().DropTable(&insumoFila{}, &cambioFila{}, &productoFila{}, &categoriaFila{}); err != nil {
		prueba.Fatal(err)
	}
	repositorio := NuevoRepositorio(base, func() time.Time { return ahora })
	if err := repositorio.Preparar(contexto); err != nil {
		prueba.Fatal(err)
	}
	return repositorio
}

func cargarTorta(prueba *testing.T, repositorio *Repositorio) dominio.Producto {
	prueba.Helper()
	tortas, _ := dominio.NuevaCategoria("categoria-tortas", "Tortas", ahora)
	if err := repositorio.CrearCategoria(contexto, tortas); err != nil {
		prueba.Fatal(err)
	}
	torta, _ := dominio.NuevoProducto("producto-torta", dominio.DatosProducto{Nombre: "Torta de limón", CategoriaID: tortas.ID, PrecioCentavos: 850000, Disponible: true}, ahora)
	torta, err := repositorio.CrearProducto(contexto, torta)
	if err != nil {
		prueba.Fatal(err)
	}
	return torta
}

func pendientes(prueba *testing.T, repositorio *Repositorio) puertos.CambiosDeIndice {
	prueba.Helper()
	cambios, err := repositorio.CambiosPendientes(contexto, 100)
	if err != nil {
		prueba.Fatal(err)
	}
	return cambios
}

func TestCategoriaConNombreUnicoSinMayusculasNiTildes(prueba *testing.T) {
	repositorio := repositorioDePrueba(prueba)
	cargarTorta(prueba, repositorio)

	otra, _ := dominio.NuevaCategoria("categoria-2", "TÓRTAS", ahora)
	if err := repositorio.CrearCategoria(contexto, otra); !errors.Is(err, dominio.ErrCategoriaRepetida) {
		prueba.Errorf("se esperaba ErrCategoriaRepetida, dio %v", err)
	}
}

func TestCrearProductoGuardaLaBandejaEnLaMismaTransaccion(prueba *testing.T) {
	repositorio := repositorioDePrueba(prueba)
	torta := cargarTorta(prueba, repositorio)

	if torta.Categoria != "Tortas" {
		prueba.Errorf("el producto creado trae su categoría: %+v", torta)
	}
	if cambios := pendientes(prueba, repositorio); len(cambios) != 1 || cambios[0].ProductoID != torta.ID || cambios[0].Operacion != puertos.IndiceGuardar || !cambios[0].CreadoEn.Equal(ahora) {
		prueba.Errorf("bandeja = %+v", cambios)
	}

	sinCategoria, _ := dominio.NuevoProducto("producto-2", dominio.DatosProducto{Nombre: "Pan", CategoriaID: "no-existe", PrecioCentavos: 100}, ahora)
	if _, err := repositorio.CrearProducto(contexto, sinCategoria); !errors.Is(err, dominio.ErrCategoriaNoEncontrada) {
		prueba.Errorf("se esperaba ErrCategoriaNoEncontrada, dio %v", err)
	}
	if cambios := pendientes(prueba, repositorio); len(cambios) != 1 {
		prueba.Errorf("si el producto no se guarda, tampoco su fila de la bandeja: %+v", cambios)
	}
}

func TestModificarRecetaEImagen(prueba *testing.T) {
	repositorio := repositorioDePrueba(prueba)
	torta := cargarTorta(prueba, repositorio)

	modificada, _ := torta.Modificar(dominio.DatosProducto{Nombre: "Torta de limón", CategoriaID: torta.CategoriaID, PrecioCentavos: 990000, Disponible: false}, ahora.Add(time.Hour))
	if _, err := repositorio.ModificarProducto(contexto, modificada); err != nil {
		prueba.Fatal(err)
	}
	conReceta, _ := modificada.ReemplazarReceta([]dominio.Insumo{{Ingrediente: "Harina", Cantidad: 500, Unidad: "g"}, {Ingrediente: "Huevo", Cantidad: 3, Unidad: "unidad"}}, ahora)
	if err := repositorio.GuardarReceta(contexto, conReceta); err != nil {
		prueba.Fatal(err)
	}
	conImagen := conReceta.CambiarImagen("/imagenes/torta.png", ahora)
	if err := repositorio.GuardarImagen(contexto, conImagen); err != nil {
		prueba.Fatal(err)
	}

	leida, err := repositorio.BuscarProducto(contexto, torta.ID)
	if err != nil {
		prueba.Fatal(err)
	}
	if leida.PrecioCentavos != 990000 || leida.Disponible || leida.ImagenURL != "/imagenes/torta.png" || len(leida.Receta) != 2 || leida.Receta[1].Ingrediente != "Huevo" {
		prueba.Errorf("leída = %+v", leida)
	}
	if cambios := pendientes(prueba, repositorio); len(cambios) != 3 {
		prueba.Errorf("alta, modificación e imagen pasan por la bandeja; la receta no: %+v", cambios)
	}

	otra, _ := conReceta.ReemplazarReceta(nil, ahora)
	repositorio.GuardarReceta(contexto, otra)
	if leida, _ := repositorio.BuscarProducto(contexto, torta.ID); len(leida.Receta) != 0 {
		prueba.Errorf("la receta nueva reemplaza a la anterior: %+v", leida.Receta)
	}
}

func TestBajaLogica(prueba *testing.T) {
	repositorio := repositorioDePrueba(prueba)
	torta := cargarTorta(prueba, repositorio)

	if err := repositorio.EliminarCategoria(contexto, torta.CategoriaID); !errors.Is(err, dominio.ErrCategoriaEnUso) {
		prueba.Errorf("una categoría con productos no se borra: %v", err)
	}
	if err := repositorio.EliminarProducto(contexto, torta.ID, ahora); err != nil {
		prueba.Fatal(err)
	}

	if _, err := repositorio.BuscarProducto(contexto, torta.ID); !errors.Is(err, dominio.ErrNoEncontrado) {
		prueba.Errorf("un producto dado de baja no se encuentra: %v", err)
	}
	if err := repositorio.EliminarProducto(contexto, torta.ID, ahora); !errors.Is(err, dominio.ErrNoEncontrado) {
		prueba.Errorf("dar de baja dos veces es no encontrado: %v", err)
	}
	if guardar, borrar := pendientes(prueba, repositorio).Resolver(); len(guardar) != 0 || len(borrar) != 1 {
		prueba.Errorf("la baja queda como borrar en la bandeja: %v %v", guardar, borrar)
	}
	if err := repositorio.EliminarCategoria(contexto, torta.CategoriaID); err != nil {
		prueba.Errorf("sin productos activos la categoría se borra: %v", err)
	}
	var fila productoFila
	repositorio.base.Unscoped().First(&fila, "id = ?", torta.ID)
	if fila.CategoriaID != nil || !fila.EliminadoEn.Valid {
		prueba.Errorf("el producto dado de baja sigue en la tabla, sin categoría: %+v", fila)
	}
}

func TestListarProductos(prueba *testing.T) {
	repositorio := repositorioDePrueba(prueba)
	torta := cargarTorta(prueba, repositorio)
	for _, nombre := range []string{"Budín", "Alfajor"} {
		producto, _ := dominio.NuevoProducto("producto-"+nombre, dominio.DatosProducto{Nombre: nombre, CategoriaID: torta.CategoriaID, PrecioCentavos: 100, Disponible: nombre == "Alfajor"}, ahora)
		repositorio.CrearProducto(contexto, producto)
	}

	compradores, err := repositorio.ListarProductos(contexto, dominio.FiltroDeCatalogo{Pagina: 1})
	if err != nil {
		prueba.Fatal(err)
	}
	if compradores.Total != 2 || compradores.Productos[0].Nombre != "Alfajor" || compradores.Productos[0].Categoria != "Tortas" {
		prueba.Errorf("solo los disponibles, por nombre: %+v", compradores)
	}
	if vendedor, _ := repositorio.ListarProductos(contexto, dominio.FiltroDeCatalogo{IncluirNoDisponibles: true, Pagina: 1}); vendedor.Total != 3 {
		prueba.Errorf("el vendedor ve también los no disponibles: %+v", vendedor)
	}
	if otraPagina, _ := repositorio.ListarProductos(contexto, dominio.FiltroDeCatalogo{Pagina: 2}); len(otraPagina.Productos) != 0 || otraPagina.Total != 2 {
		prueba.Errorf("la página 2 está vacía pero informa el total: %+v", otraPagina)
	}
	if otraCategoria, _ := repositorio.ListarProductos(contexto, dominio.FiltroDeCatalogo{CategoriaID: "otra", Pagina: 1}); otraCategoria.Total != 0 {
		prueba.Errorf("filtra por categoría: %+v", otraCategoria)
	}
}

func TestRenombrarCategoriaEncolaSusProductos(prueba *testing.T) {
	repositorio := repositorioDePrueba(prueba)
	torta := cargarTorta(prueba, repositorio)
	repositorio.QuitarCambios(contexto, pendientes(prueba, repositorio).IDs())

	categoria, _ := repositorio.BuscarCategoria(contexto, torta.CategoriaID)
	renombrada, _ := categoria.Renombrar("Tortas clásicas")
	if err := repositorio.RenombrarCategoria(contexto, renombrada); err != nil {
		prueba.Fatal(err)
	}

	if guardar, _ := pendientes(prueba, repositorio).Resolver(); len(guardar) != 1 || guardar[0] != torta.ID {
		prueba.Errorf("el producto de la categoría vuelve a la bandeja: %v", guardar)
	}
	if productos, _ := repositorio.ProductosParaIndice(contexto, []string{torta.ID, "no-existe"}); len(productos) != 1 || productos[0].Categoria != "Tortas clásicas" {
		prueba.Errorf("para el índice sale con el nombre nuevo: %+v", productos)
	}
}

func TestListarParaReindexarEnLotes(prueba *testing.T) {
	repositorio := repositorioDePrueba(prueba)
	torta := cargarTorta(prueba, repositorio)
	segundo, _ := dominio.NuevoProducto("producto-zzz", dominio.DatosProducto{Nombre: "Pan", CategoriaID: torta.CategoriaID, PrecioCentavos: 100}, ahora)
	repositorio.CrearProducto(contexto, segundo)

	primerLote, _ := repositorio.ListarParaReindexar(contexto, "", 1)
	segundoLote, _ := repositorio.ListarParaReindexar(contexto, primerLote[0].ID, 1)
	fin, _ := repositorio.ListarParaReindexar(contexto, segundoLote[0].ID, 1)

	if primerLote[0].ID != torta.ID || segundoLote[0].ID != "producto-zzz" || len(fin) != 0 {
		prueba.Errorf("lotes: %v %v %v", primerLote, segundoLote, fin)
	}
}
