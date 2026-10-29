// Package api expone los casos de uso de pedidos por HTTP. Solo se llega por el
// api-gateway (ADR-001).
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
	"net/mail"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

const largoMaximoCuerpoHTTP = 16 << 10

// Headers con la identidad del usuario.
//
// shortcut: los manda el cliente y nadie los verifica. Cuando exista la
// autenticación, los pone el api-gateway a partir del token y descarta los que
// mande el cliente; hasta entonces cualquiera puede hacerse pasar por otro.
const (
	headerIDUsuario    = "X-Id-Usuario"
	headerEmailUsuario = "X-Email-Usuario"
	headerRolUsuario   = "X-Rol-Usuario"

	rolComprador = "comprador"
	rolVendedor  = "vendedor"
	claveUsuario = "usuario"
)

// CasosDeUso es lo que este adaptador necesita de la aplicación. Lo cumple
// *aplicacion.Servicio; en los tests se usa uno falso.
type CasosDeUso interface {
	Crear(contexto context.Context, comprador dominio.Comprador, clave string, solicitud dominio.Solicitud) (dominio.Pedido, error)
	Obtener(contexto context.Context, idComprador, id string) (dominio.Pedido, error)
	ListarDeComprador(contexto context.Context, idComprador string) ([]dominio.Pedido, error)
	ListarDelDia(contexto context.Context, fecha string) ([]dominio.Pedido, error)
	Cancelar(contexto context.Context, idComprador, id string) (dominio.Pedido, error)
	CambiarEstado(contexto context.Context, id string, nuevo dominio.Estado) (dominio.Pedido, error)
	DefinirCapacidad(contexto context.Context, fecha string, capacidad int) (dominio.Dia, error)
	ListarCapacidad(contexto context.Context, desde, hasta string) ([]dominio.Dia, error)
}

// Registrar agrega las rutas al router. chequearSalud dice si las dependencias
// obligatorias responden (MongoDB).
func Registrar(router gin.IRouter, casos CasosDeUso, chequearSalud func(contexto context.Context) error) {
	manejador := &manejador{casos: casos}

	router.GET("/health", salud(chequearSalud))
	router.GET("/vivo", vivo)

	pedidos := router.Group("/pedidos", identificar)
	pedidos.POST("", soloRol(rolComprador), manejador.crear)
	pedidos.GET("", manejador.listar)
	pedidos.GET("/:id", manejador.obtener)
	pedidos.POST("/:id/cancelacion", soloRol(rolComprador), manejador.cancelar)
	pedidos.PUT("/:id/estado", soloRol(rolVendedor), manejador.cambiarEstado)

	capacidad := router.Group("/capacidad", identificar, soloRol(rolVendedor))
	capacidad.GET("", manejador.listarCapacidad)
	capacidad.PUT("/:fecha", manejador.definirCapacidad)
}

type manejador struct {
	casos CasosDeUso
}

type usuario struct {
	id    string
	email string
	rol   string
}

type cuerpoNuevoPedido struct {
	Tipo   string       `json:"tipo"`
	Items  []cuerpoItem `json:"items"`
	Fecha  string       `json:"fecha"`
	Franja string       `json:"franja"`
}

type cuerpoItem struct {
	ProductoID string `json:"productoId"`
	Cantidad   int    `json:"cantidad"`
}

type cuerpoEstado struct {
	Estado string `json:"estado"`
}

type cuerpoCapacidad struct {
	Capacidad *int `json:"capacidad"`
}

type respuestaPedido struct {
	ID                string          `json:"id"`
	Items             []respuestaItem `json:"items"`
	Fecha             string          `json:"fecha"`
	Franja            string          `json:"franja,omitempty"`
	PrecioCentavos    int64           `json:"precioCentavos"` // total del pedido
	Tipo              string          `json:"tipo"`
	Estado            string          `json:"estado"`
	MotivoCancelacion string          `json:"motivoCancelacion,omitempty"`
	RequiereRevision  bool            `json:"requiereRevision"`
	MotivoRevision    string          `json:"motivoRevision,omitempty"`
	CreadoEn          time.Time       `json:"creadoEn"`
}

// respuestaItem tiene los mismos campos que dominio.Item, así se convierten directo.
type respuestaItem struct {
	ProductoID             string `json:"productoId"`
	Nombre                 string `json:"nombreProducto"`
	Cantidad               int    `json:"cantidad"`
	PrecioUnitarioCentavos int64  `json:"precioUnitarioCentavos"`
	SubtotalCentavos       int64  `json:"subtotalCentavos"`
}

type respuestaDia struct {
	Capacidad  int `json:"capacidad"`
	Reservados int `json:"reservados"`
}

type respuestaError struct {
	Codigo       string `json:"codigo"`
	Mensaje      string `json:"mensaje"`
	PrimeraFecha string `json:"primeraFecha,omitempty"`
}

func (manejador *manejador) crear(contexto *gin.Context) {
	clave := contexto.GetHeader("Idempotency-Key")
	if clave == "" {
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", "Falta el header Idempotency-Key.")
		return
	}
	var cuerpo cuerpoNuevoPedido
	if !leerCuerpo(contexto, &cuerpo) {
		return
	}
	actual := usuarioDe(contexto)
	items := make([]dominio.ItemSolicitado, len(cuerpo.Items))
	for indice, item := range cuerpo.Items {
		items[indice] = dominio.ItemSolicitado(item)
	}
	pedido, err := manejador.casos.Crear(contexto.Request.Context(), dominio.Comprador{ID: actual.id, Email: actual.email}, clave, dominio.Solicitud{
		Tipo:   dominio.Tipo(cuerpo.Tipo),
		Items:  items,
		Fecha:  cuerpo.Fecha,
		Franja: cuerpo.Franja,
	})
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.Header("Location", "/pedidos/"+pedido.ID)
	contexto.JSON(http.StatusCreated, aRespuesta(pedido))
}

// listar devuelve los pedidos del comprador o, para el vendedor, los de un día.
func (manejador *manejador) listar(contexto *gin.Context) {
	actual := usuarioDe(contexto)
	var pedidos []dominio.Pedido
	var err error
	if actual.rol == rolVendedor {
		fecha := contexto.Query("fecha")
		if fecha == "" {
			responderError(contexto, http.StatusBadRequest, "solicitud_invalida", "Falta el parámetro fecha (AAAA-MM-DD).")
			return
		}
		pedidos, err = manejador.casos.ListarDelDia(contexto.Request.Context(), fecha)
	} else {
		pedidos, err = manejador.casos.ListarDeComprador(contexto.Request.Context(), actual.id)
	}
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	respuesta := make([]respuestaPedido, len(pedidos))
	for indice, pedido := range pedidos {
		respuesta[indice] = aRespuesta(pedido)
	}
	contexto.JSON(http.StatusOK, respuesta)
}

func (manejador *manejador) obtener(contexto *gin.Context) {
	pedido, err := manejador.casos.Obtener(contexto.Request.Context(), idCompradorDe(contexto), contexto.Param("id"))
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, aRespuesta(pedido))
}

func (manejador *manejador) cancelar(contexto *gin.Context) {
	pedido, err := manejador.casos.Cancelar(contexto.Request.Context(), usuarioDe(contexto).id, contexto.Param("id"))
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, aRespuesta(pedido))
}

func (manejador *manejador) cambiarEstado(contexto *gin.Context) {
	var cuerpo cuerpoEstado
	if !leerCuerpo(contexto, &cuerpo) {
		return
	}
	pedido, err := manejador.casos.CambiarEstado(contexto.Request.Context(), contexto.Param("id"), dominio.Estado(cuerpo.Estado))
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, aRespuesta(pedido))
}

// listarCapacidad devuelve un objeto fecha -> {capacidad, reservados}.
func (manejador *manejador) listarCapacidad(contexto *gin.Context) {
	dias, err := manejador.casos.ListarCapacidad(contexto.Request.Context(), contexto.Query("desde"), contexto.Query("hasta"))
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	respuesta := make(map[string]respuestaDia, len(dias))
	for _, dia := range dias {
		respuesta[dia.Fecha] = respuestaDia{Capacidad: dia.Capacidad, Reservados: dia.Reservados}
	}
	contexto.JSON(http.StatusOK, respuesta)
}

func (manejador *manejador) definirCapacidad(contexto *gin.Context) {
	var cuerpo cuerpoCapacidad
	if !leerCuerpo(contexto, &cuerpo) {
		return
	}
	if cuerpo.Capacidad == nil {
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", "Falta capacidad.")
		return
	}
	dia, err := manejador.casos.DefinirCapacidad(contexto.Request.Context(), contexto.Param("fecha"), *cuerpo.Capacidad)
	if errors.Is(err, dominio.ErrCapacidadMenorQueReservados) {
		responderError(contexto, http.StatusConflict, "capacidad_menor_que_reservados",
			fmt.Sprintf("La capacidad no puede ser menor a los %d ya reservados.", dia.Reservados))
		return
	}
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, respuestaDia{Capacidad: dia.Capacidad, Reservados: dia.Reservados})
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

func aRespuesta(pedido dominio.Pedido) respuestaPedido {
	items := make([]respuestaItem, len(pedido.Items))
	for indice, item := range pedido.Items {
		items[indice] = respuestaItem(item)
	}
	return respuestaPedido{
		ID:                pedido.ID,
		Items:             items,
		Fecha:             pedido.Fecha,
		Franja:            pedido.Franja,
		PrecioCentavos:    pedido.TotalCentavos,
		Tipo:              string(pedido.Tipo),
		Estado:            string(pedido.Estado),
		MotivoCancelacion: pedido.MotivoCancelacion,
		RequiereRevision:  pedido.RequiereRevision,
		MotivoRevision:    pedido.MotivoRevision,
		CreadoEn:          pedido.CreadoEn,
	}
}

// responderErrorDeDominio traduce los errores del dominio. Lo que no es un error
// conocido es una falla nuestra (por ejemplo, MongoDB caído): se registra el
// detalle y el cliente recibe 503.
func responderErrorDeDominio(contexto *gin.Context, err error) {
	var anticipacion dominio.ErrorDeAnticipacion
	switch {
	case errors.As(err, &anticipacion):
		contexto.AbortWithStatusJSON(http.StatusUnprocessableEntity, respuestaError{
			Codigo:       "anticipacion_insuficiente",
			Mensaje:      fmt.Sprintf("El producto necesita más anticipación. La primera fecha posible es %s.", anticipacion.PrimeraFecha),
			PrimeraFecha: anticipacion.PrimeraFecha,
		})
	case errors.Is(err, dominio.ErrSolicitudInvalida):
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", err.Error())
	case errors.Is(err, dominio.ErrNoEncontrado):
		responderError(contexto, http.StatusNotFound, "no_encontrado", "Pedido inexistente.")
	case errors.Is(err, dominio.ErrProductoInexistente):
		responderError(contexto, http.StatusUnprocessableEntity, "producto_inexistente", "El producto no existe o no está a la venta.")
	case errors.Is(err, dominio.ErrSinCupo):
		responderError(contexto, http.StatusConflict, "sin_cupo", "Sin cupo para ese día.")
	case errors.Is(err, dominio.ErrTransicionInvalida):
		responderError(contexto, http.StatusConflict, "transicion_invalida", "El pedido no puede pasar a ese estado. "+err.Error()+".")
	case errors.Is(err, dominio.ErrConflictoIdempotencia):
		responderError(contexto, http.StatusConflict, "conflicto_idempotencia", "La clave de idempotencia ya se usó con otro pedido.")
	case errors.Is(err, dominio.ErrModificadoAlMismoTiempo):
		responderError(contexto, http.StatusConflict, "modificado_al_mismo_tiempo", "El pedido cambió mientras se lo modificaba. Volvé a cargarlo.")
	case errors.Is(err, puertos.ErrProductoNoDisponible):
		slog.WarnContext(contexto.Request.Context(), "productos no respondió al crear un pedido", "error", err)
		responderError(contexto, http.StatusServiceUnavailable, "productos_no_disponible", "No pudimos tomar el pedido, probá en un rato.")
	default:
		slog.ErrorContext(contexto.Request.Context(), "error atendiendo una solicitud de pedidos",
			"metodo", contexto.Request.Method, "ruta", contexto.FullPath(), "error", err)
		responderError(contexto, http.StatusServiceUnavailable, "no_disponible", "Servicio temporalmente no disponible.")
	}
}

func responderError(contexto *gin.Context, estado int, codigo, mensaje string) {
	contexto.AbortWithStatusJSON(estado, respuestaError{Codigo: codigo, Mensaje: mensaje})
}

// identificar lee la identidad del usuario de los headers provisorios.
func identificar(contexto *gin.Context) {
	actual := usuario{
		id:    contexto.GetHeader(headerIDUsuario),
		email: contexto.GetHeader(headerEmailUsuario),
		rol:   contexto.GetHeader(headerRolUsuario),
	}
	if actual.id == "" || (actual.rol != rolComprador && actual.rol != rolVendedor) {
		responderError(contexto, http.StatusUnauthorized, "no_autenticado", "Faltan los datos del usuario.")
		return
	}
	if actual.rol == rolComprador {
		// El email se copia al pedido para avisarle: tiene que ser una dirección sola.
		direccion, err := mail.ParseAddress(actual.email)
		if err != nil || direccion.Name != "" || direccion.Address != actual.email {
			responderError(contexto, http.StatusUnauthorized, "no_autenticado", "Falta el email del comprador o no es válido.")
			return
		}
	}
	contexto.Set(claveUsuario, actual)
	contexto.Next()
}

func soloRol(rol string) gin.HandlerFunc {
	return func(contexto *gin.Context) {
		if usuarioDe(contexto).rol != rol {
			responderError(contexto, http.StatusForbidden, "prohibido", "Esta operación es solo para el "+rol+".")
			return
		}
		contexto.Next()
	}
}

func usuarioDe(contexto *gin.Context) usuario {
	actual, _ := contexto.Get(claveUsuario)
	return actual.(usuario)
}

// idCompradorDe devuelve el comprador para filtrar sus pedidos, o vacío si es el
// vendedor, que ve todos.
func idCompradorDe(contexto *gin.Context) string {
	actual := usuarioDe(contexto)
	if actual.rol == rolVendedor {
		return ""
	}
	return actual.id
}

func salud(chequear func(contexto context.Context) error) gin.HandlerFunc {
	return func(contexto *gin.Context) {
		if chequear != nil {
			if err := chequear(contexto.Request.Context()); err != nil {
				slog.WarnContext(contexto.Request.Context(), "chequeo de salud fallido", "error", err)
				contexto.JSON(http.StatusServiceUnavailable, gin.H{"status": "degradado", "service": "pedidos"})
				return
			}
		}
		contexto.JSON(http.StatusOK, gin.H{"status": "ok", "service": "pedidos"})
	}
}

// vivo responde si el proceso atiende, sin mirar dependencias (ADR-012).
func vivo(contexto *gin.Context) {
	contexto.JSON(http.StatusOK, gin.H{"status": "vivo", "service": "pedidos"})
}
