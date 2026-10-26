package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/adaptadores/api"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
)

var torta = dominio.Producto{
	ID: "torta", Nombre: "Torta de limón", CategoriaID: "tortas", Categoria: "Tortas", PrecioCentavos: 850000,
	AnticipacionMinimaDias: 2, Disponible: true, ImagenURL: "/imagenes/torta.png",
	Receta:   []dominio.Insumo{{Ingrediente: "Harina", Cantidad: 500, Unidad: "g"}},
	CreadoEn: time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC),
}

// casosFalsos responde lo configurado y guarda con qué lo llamaron.
type casosFalsos struct {
	err                  error
	consulta             dominio.Consulta
	incluirNoDisponibles bool
	datos                dominio.DatosProducto
	imagen               []byte
}

func (casos *casosFalsos) CrearCategoria(_ context.Context, nombre string) (dominio.Categoria, error) {
	return dominio.Categoria{ID: "tortas", Nombre: nombre}, casos.err
}
func (casos *casosFalsos) RenombrarCategoria(_ context.Context, id, nombre string) (dominio.Categoria, error) {
	return dominio.Categoria{ID: id, Nombre: nombre}, casos.err
}
func (casos *casosFalsos) EliminarCategoria(context.Context, string) error { return casos.err }
func (casos *casosFalsos) ListarCategorias(context.Context) ([]dominio.Categoria, error) {
	return []dominio.Categoria{{ID: "tortas", Nombre: "Tortas"}}, casos.err
}
func (casos *casosFalsos) CrearProducto(_ context.Context, datos dominio.DatosProducto) (dominio.Producto, error) {
	casos.datos = datos
	return torta.VistaPublica(), casos.err
}
func (casos *casosFalsos) ModificarProducto(_ context.Context, _ string, datos dominio.DatosProducto) (dominio.Producto, error) {
	casos.datos = datos
	return torta.VistaPublica(), casos.err
}
func (casos *casosFalsos) ReemplazarReceta(_ context.Context, _ string, receta []dominio.Insumo) (dominio.Producto, error) {
	conReceta := torta
	conReceta.Receta = receta
	return conReceta, casos.err
}
func (casos *casosFalsos) SubirImagen(_ context.Context, _ string, contenido []byte) (dominio.Producto, error) {
	casos.imagen = contenido
	return torta.VistaPublica(), casos.err
}
func (casos *casosFalsos) EliminarProducto(context.Context, string) error { return casos.err }
func (casos *casosFalsos) ObtenerProducto(context.Context, string) (dominio.Producto, error) {
	return torta.VistaPublica(), casos.err
}
func (casos *casosFalsos) ObtenerProductoConReceta(context.Context, string) (dominio.Producto, error) {
	return torta, casos.err
}
func (casos *casosFalsos) ListarProductos(_ context.Context, _ string, incluirNoDisponibles bool, pagina int) (dominio.PaginaDeProductos, error) {
	casos.incluirNoDisponibles = incluirNoDisponibles
	return dominio.PaginaDeProductos{Productos: []dominio.Producto{torta.VistaPublica()}, Pagina: max(pagina, 1), Total: 1}, casos.err
}
func (casos *casosFalsos) BuscarProductos(_ context.Context, consulta dominio.Consulta) (dominio.PaginaDeProductos, error) {
	casos.consulta = consulta
	return dominio.PaginaDeProductos{Pagina: 1}, casos.err
}

var vendedor = map[string]string{"X-Id-Usuario": "vendedor-1", "X-Rol-Usuario": "vendedor"}
var comprador = map[string]string{"X-Id-Usuario": "comprador-1", "X-Rol-Usuario": "comprador"}

func pedir(casos *casosFalsos, metodo, ruta string, cuerpo string, headers map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	solicitud := httptest.NewRequest(metodo, ruta, strings.NewReader(cuerpo))
	if cuerpo != "" {
		solicitud.Header.Set("Content-Type", "application/json")
	}
	return enviar(casos, solicitud, headers)
}

func enviar(casos *casosFalsos, solicitud *http.Request, headers map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api.Registrar(router, casos, nil, nil)
	for nombre, valor := range headers {
		solicitud.Header.Set(nombre, valor)
	}
	respuesta := httptest.NewRecorder()
	router.ServeHTTP(respuesta, solicitud)
	var datos map[string]any
	json.Unmarshal(respuesta.Body.Bytes(), &datos)
	return respuesta, datos
}

// pedidos lee estos campos con estos nombres
// (backend/pedidos/internal/adaptadores/productos/catalogo.go). Si este test
// falla, hay que cambiar también pedidos y ADR-003.
func TestContratoInternoConPedidos(prueba *testing.T) {
	respuesta, datos := pedir(&casosFalsos{}, http.MethodGet, "/internos/productos/torta", "", nil)

	if respuesta.Code != http.StatusOK {
		prueba.Fatalf("estado %d", respuesta.Code)
	}
	var campos []string
	for campo := range datos {
		campos = append(campos, campo)
	}
	slices.Sort(campos)
	if esperados := []string{"anticipacionMinimaDias", "disponible", "id", "nombre", "precioCentavos", "receta"}; !slices.Equal(campos, esperados) {
		prueba.Errorf("campos = %v, se esperaban %v", campos, esperados)
	}
	insumo := datos["receta"].([]any)[0].(map[string]any)
	if insumo["ingrediente"] != "Harina" || insumo["cantidad"] != float64(500) || insumo["unidad"] != "g" || datos["precioCentavos"] != float64(850000) {
		prueba.Errorf("datos = %v", datos)
	}

	if respuesta, _ := pedir(&casosFalsos{err: dominio.ErrNoEncontrado}, http.MethodGet, "/internos/productos/x", "", nil); respuesta.Code != http.StatusNotFound {
		prueba.Errorf("un producto inexistente o dado de baja es 404 para pedidos: %d", respuesta.Code)
	}
}

func TestSoloElVendedorModifica(prueba *testing.T) {
	cuerpo := `{"nombre":"Tortas"}`
	if respuesta, datos := pedir(&casosFalsos{}, http.MethodPost, "/categorias", cuerpo, nil); respuesta.Code != http.StatusUnauthorized || datos["codigo"] != "no_autenticado" {
		prueba.Errorf("sin usuario: %d %v", respuesta.Code, datos)
	}
	if respuesta, datos := pedir(&casosFalsos{}, http.MethodPost, "/categorias", cuerpo, comprador); respuesta.Code != http.StatusForbidden || datos["codigo"] != "prohibido" {
		prueba.Errorf("comprador: %d %v", respuesta.Code, datos)
	}
	if respuesta, _ := pedir(&casosFalsos{}, http.MethodGet, "/productos/torta/receta", "", comprador); respuesta.Code != http.StatusForbidden {
		prueba.Errorf("la receta es solo para el vendedor: %d", respuesta.Code)
	}
	if respuesta, _ := pedir(&casosFalsos{}, http.MethodPost, "/categorias", cuerpo, vendedor); respuesta.Code != http.StatusCreated || respuesta.Header().Get("Location") != "/categorias/tortas" {
		prueba.Errorf("vendedor: %d", respuesta.Code)
	}
}

func TestCrearProducto(prueba *testing.T) {
	casos := &casosFalsos{}
	cuerpo := `{"nombre":"Torta de limón","categoriaId":"tortas","precioCentavos":850000,"anticipacionMinimaDias":2,"disponible":true}`

	respuesta, datos := pedir(casos, http.MethodPost, "/productos", cuerpo, vendedor)

	if respuesta.Code != http.StatusCreated || respuesta.Header().Get("Location") != "/productos/torta" {
		prueba.Fatalf("estado %d", respuesta.Code)
	}
	if casos.datos.PrecioCentavos != 850000 || !casos.datos.Disponible || casos.datos.CategoriaID != "tortas" {
		prueba.Errorf("llegó %+v", casos.datos)
	}
	if datos["categoria"] != "Tortas" || datos["imagenUrl"] != "/imagenes/torta.png" || datos["receta"] != nil {
		prueba.Errorf("respuesta pública, sin receta: %v", datos)
	}
	if respuesta, _ := pedir(casos, http.MethodPost, "/productos", `{"precio":1}`, vendedor); respuesta.Code != http.StatusBadRequest {
		prueba.Errorf("un campo desconocido es 400: %d", respuesta.Code)
	}
}

func TestErroresDelDominio(prueba *testing.T) {
	casos := []struct {
		err    error
		estado int
		codigo string
	}{
		{dominio.ErrSolicitudInvalida, http.StatusBadRequest, "solicitud_invalida"},
		{dominio.ErrNoEncontrado, http.StatusNotFound, "no_encontrado"},
		{dominio.ErrCategoriaNoEncontrada, http.StatusNotFound, "categoria_no_encontrada"},
		{dominio.ErrCategoriaRepetida, http.StatusConflict, "categoria_repetida"},
		{dominio.ErrCategoriaEnUso, http.StatusConflict, "categoria_en_uso"},
		{dominio.ErrImagenInvalida, http.StatusUnprocessableEntity, "imagen_invalida"},
		{puertos.ErrBusquedaNoDisponible, http.StatusServiceUnavailable, "busqueda_no_disponible"},
		{errors.New("MySQL caído"), http.StatusServiceUnavailable, "no_disponible"},
	}
	for _, caso := range casos {
		respuesta, datos := pedir(&casosFalsos{err: caso.err}, http.MethodDelete, "/categorias/tortas", "", vendedor)
		if respuesta.Code != caso.estado || datos["codigo"] != caso.codigo {
			prueba.Errorf("%v: estado %d, código %v", caso.err, respuesta.Code, datos["codigo"])
		}
	}
	if respuesta, datos := pedir(&casosFalsos{err: errors.New("MySQL caído")}, http.MethodGet, "/productos/torta", "", nil); strings.Contains(datos["mensaje"].(string), "MySQL") || respuesta.Code != http.StatusServiceUnavailable {
		prueba.Errorf("una falla interna no muestra el detalle: %v", datos)
	}
}

func TestBuscarLeeLosParametros(prueba *testing.T) {
	casos := &casosFalsos{}

	respuesta, _ := pedir(casos, http.MethodGet, "/productos/busqueda?q=limon&categoria=tortas&precioMin=100&precioMax=900000&aptoCeliacos=true&orden=precio_asc&pagina=2&incluirNoDisponibles=true", "", comprador)

	consulta := casos.consulta
	if respuesta.Code != http.StatusOK || consulta.Texto != "limon" || consulta.CategoriaID != "tortas" || consulta.Orden != dominio.OrdenPrecioAsc || consulta.Pagina != 2 {
		prueba.Fatalf("estado %d, consulta %+v", respuesta.Code, consulta)
	}
	if *consulta.PrecioMinimoCentavos != 100 || *consulta.PrecioMaximoCentavos != 900000 || !*consulta.AptoCeliacos || consulta.IncluirNoDisponibles {
		prueba.Errorf("filtros: %+v (un comprador no ve los no disponibles)", consulta)
	}
	for _, ruta := range []string{"/productos/busqueda?precioMin=1,5", "/productos/busqueda?aptoCeliacos=quizas", "/productos/busqueda?pagina=dos"} {
		if respuesta, _ := pedir(casos, http.MethodGet, ruta, "", nil); respuesta.Code != http.StatusBadRequest {
			prueba.Errorf("%s: se esperaba 400, dio %d", ruta, respuesta.Code)
		}
	}
}

func TestCatalogoDelVendedorIncluyeNoDisponibles(prueba *testing.T) {
	casos := &casosFalsos{}

	respuesta, datos := pedir(casos, http.MethodGet, "/productos?incluirNoDisponibles=true", "", vendedor)

	if respuesta.Code != http.StatusOK || !casos.incluirNoDisponibles || datos["porPagina"] != float64(20) || datos["total"] != float64(1) {
		prueba.Errorf("estado %d, incluir %v, datos %v", respuesta.Code, casos.incluirNoDisponibles, datos)
	}
	pedir(casos, http.MethodGet, "/productos?incluirNoDisponibles=true", "", nil)
	if casos.incluirNoDisponibles {
		prueba.Error("sin ser vendedor, el parámetro se ignora")
	}
}

func TestSubirImagen(prueba *testing.T) {
	casos := &casosFalsos{}
	var cuerpo bytes.Buffer
	formulario := multipart.NewWriter(&cuerpo)
	parte, _ := formulario.CreateFormFile("imagen", "torta.png")
	parte.Write([]byte("\x89PNG\r\n\x1a\nfoto"))
	formulario.Close()
	solicitud := httptest.NewRequest(http.MethodPost, "/productos/torta/imagen", &cuerpo)
	solicitud.Header.Set("Content-Type", formulario.FormDataContentType())

	respuesta, _ := enviar(casos, solicitud, vendedor)

	if respuesta.Code != http.StatusOK || string(casos.imagen) != "\x89PNG\r\n\x1a\nfoto" {
		prueba.Errorf("estado %d, llegó %q", respuesta.Code, casos.imagen)
	}
	if respuesta, _ := pedir(casos, http.MethodPost, "/productos/torta/imagen", `{}`, vendedor); respuesta.Code != http.StatusBadRequest {
		prueba.Errorf("sin archivo es 400: %d", respuesta.Code)
	}
}

func TestSaludDependeDeMySQL(prueba *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, caso := range []struct {
		ruta   string
		falla  error
		estado int
	}{
		{"/health", nil, http.StatusOK},
		{"/health", errors.New("MySQL caído"), http.StatusServiceUnavailable},
		{"/vivo", errors.New("MySQL caído"), http.StatusOK},
	} {
		router := gin.New()
		api.Registrar(router, &casosFalsos{}, func(context.Context) error { return caso.falla }, nil)
		respuesta := httptest.NewRecorder()
		router.ServeHTTP(respuesta, httptest.NewRequest(http.MethodGet, caso.ruta, nil))
		if respuesta.Code != caso.estado {
			prueba.Errorf("%s con %v: %d", caso.ruta, caso.falla, respuesta.Code)
		}
	}
}
