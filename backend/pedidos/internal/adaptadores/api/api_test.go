package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/adaptadores/api"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

// casosFalsos responde lo configurado y guarda con qué lo llamaron.
type casosFalsos struct {
	errCrear     error
	comprador    dominio.Comprador
	solicitud    dominio.Solicitud
	errCapacidad error
}

func (casos *casosFalsos) Crear(_ context.Context, comprador dominio.Comprador, _ string, solicitud dominio.Solicitud) (dominio.Pedido, error) {
	casos.comprador, casos.solicitud = comprador, solicitud
	if casos.errCrear != nil {
		return dominio.Pedido{}, casos.errCrear
	}
	return dominio.Pedido{ID: "pedido-1", Items: []dominio.Item{{ProductoID: "medialuna", Nombre: "Medialuna", Cantidad: 2}}, TotalCentavos: 160000, Estado: dominio.EstadoConfirmado}, nil
}
func (casos *casosFalsos) Obtener(context.Context, string, string) (dominio.Pedido, error) {
	return dominio.Pedido{}, dominio.ErrNoEncontrado
}
func (casos *casosFalsos) ListarDeComprador(context.Context, string) ([]dominio.Pedido, error) {
	return nil, nil
}
func (casos *casosFalsos) ListarDelDia(context.Context, string) ([]dominio.Pedido, error) {
	return nil, nil
}
func (casos *casosFalsos) Cancelar(context.Context, string, string) (dominio.Pedido, error) {
	return dominio.Pedido{}, dominio.ErrTransicionInvalida
}
func (casos *casosFalsos) CambiarEstado(context.Context, string, dominio.Estado) (dominio.Pedido, error) {
	return dominio.Pedido{}, nil
}
func (casos *casosFalsos) DefinirCapacidad(_ context.Context, fecha string, capacidad int) (dominio.Dia, error) {
	return dominio.Dia{Fecha: fecha, Capacidad: 10, Reservados: 6}, casos.errCapacidad
}
func (casos *casosFalsos) ListarCapacidad(context.Context, string, string) ([]dominio.Dia, error) {
	return []dominio.Dia{{Fecha: "2026-10-30", Capacidad: 10, Reservados: 6}}, nil
}
func (casos *casosFalsos) SolicitarEspecial(context.Context, dominio.Comprador, string, string, string) (dominio.Pedido, error) {
	return dominio.Pedido{ID: "especial-1", Tipo: dominio.TipoEspecial, Estado: dominio.EstadoSolicitado}, nil
}
func (casos *casosFalsos) ListarEspecialesAbiertos(context.Context) ([]dominio.Pedido, error) {
	return nil, nil
}
func (casos *casosFalsos) CotizarEspecial(context.Context, string, int64) (dominio.Pedido, error) {
	return dominio.Pedido{}, nil
}
func (casos *casosFalsos) ResponderEspecial(context.Context, string, string, bool) (dominio.Pedido, error) {
	return dominio.Pedido{}, dominio.ErrCotizacionVencida
}

var comprador = map[string]string{"X-Id-Usuario": "comprador-1", "X-Email-Usuario": "cliente@ejemplo.com", "X-Rol-Usuario": "comprador"}
var vendedor = map[string]string{"X-Id-Usuario": "vendedor-1", "X-Rol-Usuario": "vendedor"}

const cuerpoPedido = `{"tipo":"programado","items":[{"productoId":"medialuna","cantidad":2}],"fecha":"2026-10-30"}`

func pedir(casos *casosFalsos, metodo, ruta, cuerpo string, headers map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api.Registrar(router, casos, nil)
	solicitud := httptest.NewRequest(metodo, ruta, strings.NewReader(cuerpo))
	if cuerpo != "" {
		solicitud.Header.Set("Content-Type", "application/json")
	}
	for nombre, valor := range headers {
		solicitud.Header.Set(nombre, valor)
	}
	respuesta := httptest.NewRecorder()
	router.ServeHTTP(respuesta, solicitud)
	var datos map[string]any
	json.Unmarshal(respuesta.Body.Bytes(), &datos)
	return respuesta, datos
}

func conClave(headers map[string]string) map[string]string {
	copia := map[string]string{"Idempotency-Key": "clave-1"}
	for nombre, valor := range headers {
		copia[nombre] = valor
	}
	return copia
}

func TestCrearPedido(prueba *testing.T) {
	casos := &casosFalsos{}

	respuesta, datos := pedir(casos, http.MethodPost, "/pedidos", cuerpoPedido, conClave(comprador))

	if respuesta.Code != http.StatusCreated || respuesta.Header().Get("Location") != "/pedidos/pedido-1" {
		prueba.Fatalf("estado %d, Location %q", respuesta.Code, respuesta.Header().Get("Location"))
	}
	if datos["precioCentavos"] != float64(160000) || datos["estado"] != "confirmado" {
		prueba.Errorf("respuesta = %v", datos)
	}
	if items := datos["items"].([]any); len(items) != 1 || items[0].(map[string]any)["nombreProducto"] != "Medialuna" {
		prueba.Errorf("ítems = %v", datos["items"])
	}
	if casos.comprador.Email != "cliente@ejemplo.com" || casos.solicitud.Fecha != "2026-10-30" || casos.solicitud.Items[0].Cantidad != 2 {
		prueba.Errorf("llegó %+v %+v", casos.comprador, casos.solicitud)
	}
}

func TestErroresAlCrear(prueba *testing.T) {
	casos := []struct {
		nombre  string
		headers map[string]string
		cuerpo  string
		err     error
		estado  int
		codigo  string
	}{
		{"sin usuario", map[string]string{"Idempotency-Key": "clave-1"}, cuerpoPedido, nil, http.StatusUnauthorized, "no_autenticado"},
		{"comprador sin email", conClave(map[string]string{"X-Id-Usuario": "comprador-1", "X-Rol-Usuario": "comprador"}), cuerpoPedido, nil, http.StatusUnauthorized, "no_autenticado"},
		{"vendedor no encarga", conClave(vendedor), cuerpoPedido, nil, http.StatusForbidden, "prohibido"},
		{"sin Idempotency-Key", comprador, cuerpoPedido, nil, http.StatusBadRequest, "solicitud_invalida"},
		{"campo desconocido", conClave(comprador), `{"tipo":"inmediato","precio":1}`, nil, http.StatusBadRequest, "solicitud_invalida"},
		{"sin cupo", conClave(comprador), cuerpoPedido, dominio.ErrSinCupo, http.StatusConflict, "sin_cupo"},
		{"producto inexistente", conClave(comprador), cuerpoPedido, dominio.ErrProductoInexistente, http.StatusUnprocessableEntity, "producto_inexistente"},
		{"productos caído", conClave(comprador), cuerpoPedido, puertos.ErrProductoNoDisponible, http.StatusServiceUnavailable, "productos_no_disponible"},
		{"otra falla", conClave(comprador), cuerpoPedido, context.DeadlineExceeded, http.StatusServiceUnavailable, "no_disponible"},
	}
	for _, caso := range casos {
		prueba.Run(caso.nombre, func(prueba *testing.T) {
			respuesta, datos := pedir(&casosFalsos{errCrear: caso.err}, http.MethodPost, "/pedidos", caso.cuerpo, caso.headers)
			if respuesta.Code != caso.estado || datos["codigo"] != caso.codigo {
				prueba.Errorf("estado %d, código %v; se esperaba %d %s", respuesta.Code, datos["codigo"], caso.estado, caso.codigo)
			}
		})
	}
}

func TestAnticipacionInformaLaPrimeraFecha(prueba *testing.T) {
	casos := &casosFalsos{errCrear: dominio.ErrorDeAnticipacion{PrimeraFecha: "2026-10-29"}}

	respuesta, datos := pedir(casos, http.MethodPost, "/pedidos", cuerpoPedido, conClave(comprador))

	if respuesta.Code != http.StatusUnprocessableEntity || datos["primeraFecha"] != "2026-10-29" {
		prueba.Errorf("estado %d, datos %v", respuesta.Code, datos)
	}
}

func TestCapacidad(prueba *testing.T) {
	respuesta, datos := pedir(&casosFalsos{}, http.MethodGet, "/capacidad?desde=2026-10-30&hasta=2026-10-31", "", vendedor)
	if respuesta.Code != http.StatusOK || datos["2026-10-30"] == nil {
		prueba.Errorf("estado %d, datos %v", respuesta.Code, datos)
	}

	if respuesta, _ := pedir(&casosFalsos{}, http.MethodPut, "/capacidad/2026-10-30", `{"capacidad":5}`, comprador); respuesta.Code != http.StatusForbidden {
		prueba.Errorf("un comprador no define capacidad: %d", respuesta.Code)
	}

	casos := &casosFalsos{errCapacidad: dominio.ErrCapacidadMenorQueReservados}
	respuesta, datos = pedir(casos, http.MethodPut, "/capacidad/2026-10-30", `{"capacidad":5}`, vendedor)
	if respuesta.Code != http.StatusConflict || !strings.Contains(datos["mensaje"].(string), "6") {
		prueba.Errorf("estado %d, datos %v", respuesta.Code, datos)
	}
}

func TestCancelarEnProduccion(prueba *testing.T) {
	respuesta, datos := pedir(&casosFalsos{}, http.MethodPost, "/pedidos/pedido-1/cancelacion", "", comprador)

	if respuesta.Code != http.StatusConflict || datos["codigo"] != "transicion_invalida" {
		prueba.Errorf("estado %d, datos %v", respuesta.Code, datos)
	}
}

func TestRutasDeEspeciales(prueba *testing.T) {
	casos := map[string]struct {
		metodo, ruta, cuerpo string
		headers              map[string]string
		estado               int
	}{
		"solicitar sin clave":       {http.MethodPost, "/pedidos/especiales", `{"descripcion":"Torta","fecha":"2026-11-05"}`, comprador, http.StatusBadRequest},
		"el vendedor no solicita":   {http.MethodPost, "/pedidos/especiales", `{}`, vendedor, http.StatusForbidden},
		"el comprador no cotiza":    {http.MethodPut, "/pedidos/especiales/e1/cotizacion", `{"precioCentavos":100}`, comprador, http.StatusForbidden},
		"el comprador no lista":     {http.MethodGet, "/pedidos/especiales", "", comprador, http.StatusForbidden},
		"el vendedor lista":         {http.MethodGet, "/pedidos/especiales", "", vendedor, http.StatusOK},
		"cotización vencida es 409": {http.MethodPost, "/pedidos/especiales/e1/aceptacion", "", comprador, http.StatusConflict},
	}
	for nombre, caso := range casos {
		prueba.Run(nombre, func(prueba *testing.T) {
			if respuesta, datos := pedir(&casosFalsos{}, caso.metodo, caso.ruta, caso.cuerpo, caso.headers); respuesta.Code != caso.estado {
				prueba.Errorf("estado %d, se esperaba %d (%v)", respuesta.Code, caso.estado, datos)
			}
		})
	}
	headers := map[string]string{"Idempotency-Key": "clave-1"}
	for nombre, valor := range comprador {
		headers[nombre] = valor
	}
	if respuesta, datos := pedir(&casosFalsos{}, http.MethodPost, "/pedidos/especiales", `{"descripcion":"Torta","fecha":"2026-11-05"}`, headers); respuesta.Code != http.StatusCreated || datos["estado"] != "solicitado" {
		prueba.Errorf("solicitar: estado %d, datos %v", respuesta.Code, datos)
	}
	if _, datos := pedir(&casosFalsos{}, http.MethodPost, "/pedidos/especiales/e1/aceptacion", "", comprador); datos["codigo"] != "cotizacion_vencida" {
		prueba.Errorf("se esperaba cotizacion_vencida, llegó %v", datos)
	}
}

func TestVendedorListaPorFecha(prueba *testing.T) {
	if respuesta, _ := pedir(&casosFalsos{}, http.MethodGet, "/pedidos", "", vendedor); respuesta.Code != http.StatusBadRequest {
		prueba.Errorf("sin fecha se esperaba 400, dio %d", respuesta.Code)
	}
	if respuesta, _ := pedir(&casosFalsos{}, http.MethodGet, "/pedidos?fecha=2026-10-30", "", vendedor); respuesta.Code != http.StatusOK {
		prueba.Errorf("con fecha se esperaba 200, dio %d", respuesta.Code)
	}
}
