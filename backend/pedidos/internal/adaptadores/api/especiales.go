package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
)

type cuerpoEspecial struct {
	Descripcion string `json:"descripcion"`
	Fecha       string `json:"fecha"`
}

type cuerpoCotizacion struct {
	PrecioCentavos int64 `json:"precioCentavos"`
}

func (manejador *manejador) solicitarEspecial(contexto *gin.Context) {
	clave := contexto.GetHeader("Idempotency-Key")
	if clave == "" {
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", "Falta el header Idempotency-Key.")
		return
	}
	var cuerpo cuerpoEspecial
	if !leerCuerpo(contexto, &cuerpo) {
		return
	}
	actual := usuarioDe(contexto)
	pedido, err := manejador.casos.SolicitarEspecial(contexto.Request.Context(), dominio.Comprador{ID: actual.id, Email: actual.email}, clave, cuerpo.Descripcion, cuerpo.Fecha)
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.Header("Location", "/pedidos/"+pedido.ID)
	contexto.JSON(http.StatusCreated, aRespuesta(pedido))
}

func (manejador *manejador) listarEspeciales(contexto *gin.Context) {
	pedidos, err := manejador.casos.ListarEspecialesAbiertos(contexto.Request.Context())
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

func (manejador *manejador) cotizarEspecial(contexto *gin.Context) {
	var cuerpo cuerpoCotizacion
	if !leerCuerpo(contexto, &cuerpo) {
		return
	}
	pedido, err := manejador.casos.CotizarEspecial(contexto.Request.Context(), contexto.Param("id"), cuerpo.PrecioCentavos)
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, aRespuesta(pedido))
}

func (manejador *manejador) responderEspecial(acepta bool) gin.HandlerFunc {
	return func(contexto *gin.Context) {
		pedido, err := manejador.casos.ResponderEspecial(contexto.Request.Context(), usuarioDe(contexto).id, contexto.Param("id"), acepta)
		if err != nil {
			responderErrorDeDominio(contexto, err)
			return
		}
		contexto.JSON(http.StatusOK, aRespuesta(pedido))
	}
}
