// Package api expone los casos de uso de productos por HTTP. Se llega solo por el
// api-gateway (ADR-001), salvo /internos, que usa pedidos dentro del compose y el
// gateway no enruta.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
)

const (
	nombreServicio        = "productos"
	largoMaximoCuerpoHTTP = 16 << 10
	// largoMaximoSubida deja lugar para los datos del formulario además de la imagen.
	largoMaximoSubida = dominio.MaximoBytesImagen + 64<<10
)

// Headers con la identidad del usuario. Los pone el api-gateway a partir del
// token y descarta los que mande el cliente (ADR-016).
const (
	headerIDUsuario  = "X-Id-Usuario"
	headerRolUsuario = "X-Rol-Usuario"
	rolVendedor      = "vendedor"
)

// CasosDeUso es lo que este adaptador necesita de la aplicación. Lo cumple
// *aplicacion.Servicio; en los tests se usa uno falso.
type CasosDeUso interface {
	CrearCategoria(contexto context.Context, nombre string) (dominio.Categoria, error)
	RenombrarCategoria(contexto context.Context, id, nombre string) (dominio.Categoria, error)
	EliminarCategoria(contexto context.Context, id string) error
	ListarCategorias(contexto context.Context) ([]dominio.Categoria, error)

	CrearProducto(contexto context.Context, datos dominio.DatosProducto) (dominio.Producto, error)
	ModificarProducto(contexto context.Context, id string, datos dominio.DatosProducto) (dominio.Producto, error)
	ReemplazarReceta(contexto context.Context, id string, receta []dominio.Insumo) (dominio.Producto, error)
	SubirImagen(contexto context.Context, id string, contenido []byte) (dominio.Producto, error)
	EliminarProducto(contexto context.Context, id string) error
	ObtenerProducto(contexto context.Context, id string) (dominio.Producto, error)
	ObtenerProductoConReceta(contexto context.Context, id string) (dominio.Producto, error)
	ListarProductos(contexto context.Context, categoriaID string, incluirNoDisponibles bool, pagina int) (dominio.PaginaDeProductos, error)
	BuscarProductos(contexto context.Context, consulta dominio.Consulta) (dominio.PaginaDeProductos, error)
}

// Registrar agrega las rutas al router. chequearSalud dice si MySQL responde;
// imagenes es la carpeta de las fotos, que se sirven en /imagenes.
func Registrar(router gin.IRouter, casos CasosDeUso, chequearSalud func(contexto context.Context) error, imagenes http.FileSystem) {
	manejador := &manejador{casos: casos}

	router.GET("/health", salud(chequearSalud))
	router.GET("/vivo", vivo)

	categorias := router.Group("/categorias")
	categorias.GET("", manejador.listarCategorias)
	categorias.POST("", soloVendedor, manejador.crearCategoria)
	categorias.PUT("/:id", soloVendedor, manejador.renombrarCategoria)
	categorias.DELETE("/:id", soloVendedor, manejador.eliminarCategoria)

	productos := router.Group("/productos")
	productos.GET("", manejador.listarProductos)
	productos.GET("/busqueda", manejador.buscar)
	productos.GET("/:id", manejador.obtener)
	productos.POST("", soloVendedor, manejador.crearProducto)
	productos.PUT("/:id", soloVendedor, manejador.modificarProducto)
	productos.DELETE("/:id", soloVendedor, manejador.eliminarProducto)
	productos.GET("/:id/receta", soloVendedor, manejador.obtenerReceta)
	productos.PUT("/:id/receta", soloVendedor, manejador.reemplazarReceta)
	productos.POST("/:id/imagen", soloVendedor, manejador.subirImagen)

	if imagenes != nil {
		router.StaticFS("/imagenes", imagenes)
	}

	// Para pedidos, que cobra este precio: sin caché (ADR-009). Contrato fijado en
	// el test TestContratoInternoConPedidos y en ADR-003.
	router.GET("/internos/productos/:id", manejador.obtenerInterno)
}

type manejador struct {
	casos CasosDeUso
}

type respuestaError struct {
	Codigo  string `json:"codigo"`
	Mensaje string `json:"mensaje"`
}

type respuestaCategoria struct {
	ID     string `json:"id"`
	Nombre string `json:"nombre"`
}

type respuestaProducto struct {
	ID                     string         `json:"id"`
	Nombre                 string         `json:"nombre"`
	Descripcion            string         `json:"descripcion"`
	CategoriaID            string         `json:"categoriaId"`
	Categoria              string         `json:"categoria"`
	PrecioCentavos         int64          `json:"precioCentavos"`
	AnticipacionMinimaDias int            `json:"anticipacionMinimaDias"`
	AptoCeliacos           bool           `json:"aptoCeliacos"`
	Disponible             bool           `json:"disponible"`
	ImagenURL              string         `json:"imagenUrl"`
	Receta                 []cuerpoInsumo `json:"receta,omitempty"`
	CreadoEn               time.Time      `json:"creadoEn"`
	ActualizadoEn          *time.Time     `json:"actualizadoEn,omitempty"`
}

type respuestaPagina struct {
	Productos []respuestaProducto `json:"productos"`
	Pagina    int                 `json:"pagina"`
	PorPagina int                 `json:"porPagina"`
	Total     int                 `json:"total"`
}

// cuerpoInsumo tiene los mismos campos que dominio.Insumo, así se convierten directo.
type cuerpoInsumo struct {
	Ingrediente string `json:"ingrediente"`
	Cantidad    int64  `json:"cantidad"`
	Unidad      string `json:"unidad"`
}

type cuerpoCategoria struct {
	Nombre string `json:"nombre"`
}

type cuerpoProducto struct {
	Nombre                 string `json:"nombre"`
	Descripcion            string `json:"descripcion"`
	CategoriaID            string `json:"categoriaId"`
	PrecioCentavos         int64  `json:"precioCentavos"`
	AnticipacionMinimaDias int    `json:"anticipacionMinimaDias"`
	AptoCeliacos           bool   `json:"aptoCeliacos"`
	Disponible             bool   `json:"disponible"`
}

type cuerpoReceta struct {
	Receta []cuerpoInsumo `json:"receta"`
}

// respuestaInterna es el contrato con pedidos: lo lee
// backend/pedidos/internal/adaptadores/productos/catalogo.go.
type respuestaInterna struct {
	ID                     string         `json:"id"`
	Nombre                 string         `json:"nombre"`
	PrecioCentavos         int64          `json:"precioCentavos"`
	AnticipacionMinimaDias int            `json:"anticipacionMinimaDias"`
	Disponible             bool           `json:"disponible"`
	Receta                 []cuerpoInsumo `json:"receta"`
}

func aRespuesta(producto dominio.Producto) respuestaProducto {
	respuesta := respuestaProducto{
		ID:                     producto.ID,
		Nombre:                 producto.Nombre,
		Descripcion:            producto.Descripcion,
		CategoriaID:            producto.CategoriaID,
		Categoria:              producto.Categoria,
		PrecioCentavos:         producto.PrecioCentavos,
		AnticipacionMinimaDias: producto.AnticipacionMinimaDias,
		AptoCeliacos:           producto.AptoCeliacos,
		Disponible:             producto.Disponible,
		ImagenURL:              producto.ImagenURL,
		Receta:                 aInsumos(producto.Receta),
		CreadoEn:               producto.CreadoEn,
	}
	if !producto.ActualizadoEn.IsZero() { // el índice de búsqueda no la guarda
		respuesta.ActualizadoEn = &producto.ActualizadoEn
	}
	return respuesta
}

func aInsumos(receta []dominio.Insumo) []cuerpoInsumo {
	insumos := make([]cuerpoInsumo, len(receta))
	for posicion, insumo := range receta {
		insumos[posicion] = cuerpoInsumo(insumo)
	}
	return insumos
}

func aPagina(pagina dominio.PaginaDeProductos) respuestaPagina {
	productos := make([]respuestaProducto, len(pagina.Productos))
	for posicion, producto := range pagina.Productos {
		productos[posicion] = aRespuesta(producto)
	}
	return respuestaPagina{Productos: productos, Pagina: pagina.Pagina, PorPagina: dominio.TamanioPagina, Total: pagina.Total}
}

// Categorías

func (manejador *manejador) listarCategorias(contexto *gin.Context) {
	categorias, err := manejador.casos.ListarCategorias(contexto.Request.Context())
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	respuesta := make([]respuestaCategoria, len(categorias))
	for posicion, categoria := range categorias {
		respuesta[posicion] = respuestaCategoria{ID: categoria.ID, Nombre: categoria.Nombre}
	}
	contexto.JSON(http.StatusOK, respuesta)
}

func (manejador *manejador) crearCategoria(contexto *gin.Context) {
	var cuerpo cuerpoCategoria
	if !leerCuerpo(contexto, &cuerpo) {
		return
	}
	categoria, err := manejador.casos.CrearCategoria(contexto.Request.Context(), cuerpo.Nombre)
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.Header("Location", "/categorias/"+categoria.ID)
	contexto.JSON(http.StatusCreated, respuestaCategoria{ID: categoria.ID, Nombre: categoria.Nombre})
}

func (manejador *manejador) renombrarCategoria(contexto *gin.Context) {
	var cuerpo cuerpoCategoria
	if !leerCuerpo(contexto, &cuerpo) {
		return
	}
	categoria, err := manejador.casos.RenombrarCategoria(contexto.Request.Context(), contexto.Param("id"), cuerpo.Nombre)
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, respuestaCategoria{ID: categoria.ID, Nombre: categoria.Nombre})
}

func (manejador *manejador) eliminarCategoria(contexto *gin.Context) {
	if err := manejador.casos.EliminarCategoria(contexto.Request.Context(), contexto.Param("id")); err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.Status(http.StatusNoContent)
}

// Productos

// listarProductos es el catálogo (C2). incluirNoDisponibles=true vale solo para el vendedor.
func (manejador *manejador) listarProductos(contexto *gin.Context) {
	pagina, ok := numeroOpcional(contexto, "pagina")
	if !ok {
		return
	}
	incluirNoDisponibles := esVendedor(contexto) && contexto.Query("incluirNoDisponibles") == "true"
	resultado, err := manejador.casos.ListarProductos(contexto.Request.Context(), contexto.Query("categoria"), incluirNoDisponibles, pagina)
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, aPagina(resultado))
}

// buscar es la búsqueda con texto, filtros y orden (C3). Devuelve también los no
// disponibles: el frontend manda disponible=true por defecto (ADR-008).
func (manejador *manejador) buscar(contexto *gin.Context) {
	consulta := dominio.Consulta{
		Texto:     contexto.Query("q"),
		Categoria: contexto.Query("categoria"),
		Orden:     dominio.Orden(contexto.Query("orden")),
	}
	var ok bool
	if consulta.Pagina, ok = numeroOpcional(contexto, "pagina"); !ok {
		return
	}
	if consulta.PrecioMinimoCentavos, ok = centavosOpcionales(contexto, "precioMin"); !ok {
		return
	}
	if consulta.PrecioMaximoCentavos, ok = centavosOpcionales(contexto, "precioMax"); !ok {
		return
	}
	if consulta.AptoCeliacos, ok = verdaderoOFalso(contexto, "aptoCeliacos"); !ok {
		return
	}
	if consulta.Disponible, ok = verdaderoOFalso(contexto, "disponible"); !ok {
		return
	}
	resultado, err := manejador.casos.BuscarProductos(contexto.Request.Context(), consulta)
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, aPagina(resultado))
}

func (manejador *manejador) obtener(contexto *gin.Context) {
	producto, err := manejador.casos.ObtenerProducto(contexto.Request.Context(), contexto.Param("id"))
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, aRespuesta(producto))
}

func (manejador *manejador) crearProducto(contexto *gin.Context) {
	var cuerpo cuerpoProducto
	if !leerCuerpo(contexto, &cuerpo) {
		return
	}
	producto, err := manejador.casos.CrearProducto(contexto.Request.Context(), dominio.DatosProducto(cuerpo))
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.Header("Location", "/productos/"+producto.ID)
	contexto.JSON(http.StatusCreated, aRespuesta(producto))
}

func (manejador *manejador) modificarProducto(contexto *gin.Context) {
	var cuerpo cuerpoProducto
	if !leerCuerpo(contexto, &cuerpo) {
		return
	}
	producto, err := manejador.casos.ModificarProducto(contexto.Request.Context(), contexto.Param("id"), dominio.DatosProducto(cuerpo))
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, aRespuesta(producto))
}

func (manejador *manejador) eliminarProducto(contexto *gin.Context) {
	if err := manejador.casos.EliminarProducto(contexto.Request.Context(), contexto.Param("id")); err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.Status(http.StatusNoContent)
}

func (manejador *manejador) obtenerReceta(contexto *gin.Context) {
	producto, err := manejador.casos.ObtenerProductoConReceta(contexto.Request.Context(), contexto.Param("id"))
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, cuerpoReceta{Receta: aInsumos(producto.Receta)})
}

func (manejador *manejador) reemplazarReceta(contexto *gin.Context) {
	var cuerpo cuerpoReceta
	if !leerCuerpo(contexto, &cuerpo) {
		return
	}
	receta := make([]dominio.Insumo, len(cuerpo.Receta))
	for posicion, insumo := range cuerpo.Receta {
		receta[posicion] = dominio.Insumo(insumo)
	}
	producto, err := manejador.casos.ReemplazarReceta(contexto.Request.Context(), contexto.Param("id"), receta)
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, cuerpoReceta{Receta: aInsumos(producto.Receta)})
}

// subirImagen recibe un formulario multipart con el archivo en el campo "imagen".
// El tipo lo decide el dominio mirando el contenido, no el nombre del archivo.
func (manejador *manejador) subirImagen(contexto *gin.Context) {
	contexto.Request.Body = http.MaxBytesReader(contexto.Writer, contexto.Request.Body, largoMaximoSubida)
	archivo, err := contexto.FormFile("imagen")
	if err != nil {
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida",
			fmt.Sprintf("Falta el archivo en el campo \"imagen\" del formulario, o pesa más de %d MB.", dominio.MaximoBytesImagen>>20))
		return
	}
	abierto, err := archivo.Open()
	if err != nil {
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", "No se pudo leer el archivo.")
		return
	}
	defer abierto.Close()
	contenido, err := io.ReadAll(io.LimitReader(abierto, dominio.MaximoBytesImagen+1))
	if err != nil {
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", "No se pudo leer el archivo.")
		return
	}
	producto, err := manejador.casos.SubirImagen(contexto.Request.Context(), contexto.Param("id"), contenido)
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, aRespuesta(producto))
}

func (manejador *manejador) obtenerInterno(contexto *gin.Context) {
	producto, err := manejador.casos.ObtenerProductoConReceta(contexto.Request.Context(), contexto.Param("id"))
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, respuestaInterna{
		ID:                     producto.ID,
		Nombre:                 producto.Nombre,
		PrecioCentavos:         producto.PrecioCentavos,
		AnticipacionMinimaDias: producto.AnticipacionMinimaDias,
		Disponible:             producto.Disponible,
		Receta:                 aInsumos(producto.Receta),
	})
}

// Ayudas

// numeroOpcional lee un entero de la query; si no está, vale 0. Si es inválido, ya respondió.
func numeroOpcional(contexto *gin.Context, nombre string) (int, bool) {
	texto := contexto.Query(nombre)
	if texto == "" {
		return 0, true
	}
	numero, err := strconv.Atoi(texto)
	if err != nil {
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", nombre+" tiene que ser un número entero.")
		return 0, false
	}
	return numero, true
}

func centavosOpcionales(contexto *gin.Context, nombre string) (*int64, bool) {
	texto := contexto.Query(nombre)
	if texto == "" {
		return nil, true
	}
	centavos, err := strconv.ParseInt(texto, 10, 64)
	if err != nil {
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", nombre+" va en centavos, como número entero.")
		return nil, false
	}
	return &centavos, true
}

// verdaderoOFalso lee un filtro true/false opcional de la query. Si es inválido, ya respondió.
func verdaderoOFalso(contexto *gin.Context, nombre string) (*bool, bool) {
	texto := contexto.Query(nombre)
	if texto == "" {
		return nil, true
	}
	valor, err := strconv.ParseBool(texto)
	if err != nil {
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", nombre+" tiene que ser true o false.")
		return nil, false
	}
	return &valor, true
}

func esVendedor(contexto *gin.Context) bool {
	return contexto.GetHeader(headerIDUsuario) != "" && contexto.GetHeader(headerRolUsuario) == rolVendedor
}

func soloVendedor(contexto *gin.Context) {
	switch {
	case contexto.GetHeader(headerIDUsuario) == "":
		responderError(contexto, http.StatusUnauthorized, "no_autenticado", "Faltan los datos del usuario.")
	case contexto.GetHeader(headerRolUsuario) != rolVendedor:
		responderError(contexto, http.StatusForbidden, "prohibido", "Esta operación es solo para el vendedor.")
	default:
		contexto.Next()
	}
}

// leerCuerpo decodifica el JSON sin campos desconocidos. Si falla, ya respondió.
func leerCuerpo(contexto *gin.Context, destino any) bool {
	tipo, _, err := mime.ParseMediaType(contexto.GetHeader("Content-Type"))
	if err != nil || tipo != "application/json" {
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", "El cuerpo tiene que ser JSON (Content-Type: application/json).")
		return false
	}
	contenido, err := io.ReadAll(http.MaxBytesReader(contexto.Writer, contexto.Request.Body, largoMaximoCuerpoHTTP))
	if err != nil {
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", fmt.Sprintf("El cuerpo supera los %d KiB.", largoMaximoCuerpoHTTP>>10))
		return false
	}
	decodificador := json.NewDecoder(bytes.NewReader(contenido))
	decodificador.DisallowUnknownFields()
	if err := decodificador.Decode(destino); err != nil {
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", fmt.Sprintf("JSON inválido: %v.", err))
		return false
	}
	return true
}

// responderErrorDeDominio traduce los errores conocidos. Lo demás es una falla
// nuestra (por ejemplo, MySQL caído): se registra el detalle y el cliente recibe 503.
func responderErrorDeDominio(contexto *gin.Context, err error) {
	switch {
	case errors.Is(err, dominio.ErrSolicitudInvalida):
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", err.Error())
	case errors.Is(err, dominio.ErrNoEncontrado):
		responderError(contexto, http.StatusNotFound, "no_encontrado", "Producto inexistente.")
	case errors.Is(err, dominio.ErrCategoriaNoEncontrada):
		responderError(contexto, http.StatusNotFound, "categoria_no_encontrada", "Categoría inexistente.")
	case errors.Is(err, dominio.ErrCategoriaRepetida):
		responderError(contexto, http.StatusConflict, "categoria_repetida", "Ya existe una categoría con ese nombre.")
	case errors.Is(err, dominio.ErrCategoriaEnUso):
		responderError(contexto, http.StatusConflict, "categoria_en_uso", "La categoría tiene productos: primero cambialos de categoría o dalos de baja.")
	case errors.Is(err, dominio.ErrImagenInvalida):
		responderError(contexto, http.StatusUnprocessableEntity, "imagen_invalida", err.Error())
	case errors.Is(err, puertos.ErrBusquedaNoDisponible):
		slog.WarnContext(contexto.Request.Context(), "búsqueda no disponible", "error", err)
		responderError(contexto, http.StatusServiceUnavailable, "busqueda_no_disponible", "La búsqueda no está disponible. Mientras tanto se puede ver el catálogo por categoría.")
	default:
		slog.ErrorContext(contexto.Request.Context(), "error al atender la solicitud", "error", err)
		responderError(contexto, http.StatusServiceUnavailable, "no_disponible", "No pudimos atender la solicitud. Probá en un rato.")
	}
}

func responderError(contexto *gin.Context, estado int, codigo, mensaje string) {
	contexto.AbortWithStatusJSON(estado, respuestaError{Codigo: codigo, Mensaje: mensaje})
}

func salud(chequear func(contexto context.Context) error) gin.HandlerFunc {
	return func(contexto *gin.Context) {
		if chequear != nil {
			if err := chequear(contexto.Request.Context()); err != nil {
				slog.WarnContext(contexto.Request.Context(), "chequeo de salud fallido", "error", err)
				contexto.JSON(http.StatusServiceUnavailable, gin.H{"status": "degradado", "service": nombreServicio})
				return
			}
		}
		contexto.JSON(http.StatusOK, gin.H{"status": "ok", "service": nombreServicio})
	}
}

// vivo responde si el proceso atiende, sin mirar dependencias (ADR-012).
func vivo(contexto *gin.Context) {
	contexto.JSON(http.StatusOK, gin.H{"status": "vivo", "service": nombreServicio})
}
