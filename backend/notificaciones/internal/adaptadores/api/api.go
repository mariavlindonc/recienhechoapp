// Package api expone la capacidad de notificaciones por HTTP, tal como la describe
// el contrato docs/contracts/notificaciones.yaml.
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
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/dominio"
)

// largoMaximoCuerpoHTTP limita lo que se lee de un POST. El cuerpo de la
// notificación admite 5000 caracteres; con el resto de los campos y los escapes
// de JSON, 64 KiB sobra.
const largoMaximoCuerpoHTTP = 64 << 10

// claveGrupo es donde el middleware de autenticación deja el grupo del pedido.
const claveGrupo = "grupo"

// CasosDeUso es lo que este adaptador necesita de la aplicación. Lo cumple
// *aplicacion.Servicio; en los tests se usa uno falso.
type CasosDeUso interface {
	Crear(contexto context.Context, grupo, clave string, solicitud dominio.Solicitud) (dominio.Notificacion, error)
	Obtener(contexto context.Context, grupo, id string) (dominio.Notificacion, error)
}

// Configuracion del adaptador.
type Configuracion struct {
	// ClavesAPI asocia cada clave con el grupo dueño (clave -> grupo).
	ClavesAPI map[string]string
	// ChequearSalud dice si las dependencias responden (por ejemplo, MongoDB).
	ChequearSalud func(contexto context.Context) error
}

// Registrar agrega las rutas al router. Los middlewares generales (logs, trazas)
// se agregan en main antes de llamar a Registrar. El límite de solicitudes por
// minuto lo aplica el api-gateway (ADR-004, ADR-012).
func Registrar(router gin.IRouter, casos CasosDeUso, configuracion Configuracion) {
	manejador := &manejador{casos: casos}

	router.GET("/health", salud(configuracion.ChequearSalud))
	router.GET("/vivo", vivo)

	notificaciones := router.Group("/notificaciones", autenticar(configuracion.ClavesAPI))
	notificaciones.POST("", manejador.crear)
	notificaciones.GET("/:id", manejador.obtener)
}

type manejador struct {
	casos CasosDeUso
}

// cuerpoNuevaNotificacion es el esquema NuevaNotificacion del contrato.
type cuerpoNuevaNotificacion struct {
	Canal        string `json:"canal"`
	Destinatario string `json:"destinatario"`
	Asunto       string `json:"asunto"`
	Cuerpo       string `json:"cuerpo"`
}

// respuestaNotificacion es el esquema Notificacion del contrato. No incluye el
// cuerpo del mensaje.
type respuestaNotificacion struct {
	ID           string     `json:"id"`
	Canal        string     `json:"canal"`
	Destinatario string     `json:"destinatario"`
	Asunto       string     `json:"asunto"`
	Estado       string     `json:"estado"`
	Motivo       string     `json:"motivo,omitempty"`
	CreadaEn     time.Time  `json:"creadaEn"`
	EnviadaEn    *time.Time `json:"enviadaEn,omitempty"`
}

type respuestaError struct {
	Codigo  string `json:"codigo"`
	Mensaje string `json:"mensaje"`
}

func (manejador *manejador) crear(contexto *gin.Context) {
	clave := contexto.GetHeader("Idempotency-Key")
	if clave == "" {
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", "Falta el header Idempotency-Key.")
		return
	}
	cuerpo, err := leerCuerpo(contexto)
	if err != nil {
		var problema errorDelPedido
		if errors.As(err, &problema) {
			responderError(contexto, http.StatusBadRequest, "solicitud_invalida", problema.mensaje)
			return
		}
		responderErrorDeDominio(contexto, err)
		return
	}

	notificacion, err := manejador.casos.Crear(contexto.Request.Context(), contexto.GetString(claveGrupo), clave, dominio.Solicitud{
		Canal:        dominio.Canal(cuerpo.Canal),
		Destinatario: cuerpo.Destinatario,
		Asunto:       cuerpo.Asunto,
		Cuerpo:       cuerpo.Cuerpo,
	})
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.Header("Location", "/notificaciones/"+notificacion.ID)
	contexto.JSON(http.StatusAccepted, aRespuesta(notificacion))
}

func (manejador *manejador) obtener(contexto *gin.Context) {
	notificacion, err := manejador.casos.Obtener(contexto.Request.Context(), contexto.GetString(claveGrupo), contexto.Param("id"))
	if err != nil {
		responderErrorDeDominio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, aRespuesta(notificacion))
}

// errorDelPedido es un problema del pedido HTTP que se le explica al consumidor:
// su mensaje es el texto que va en la respuesta del contrato, por eso va con
// mayúscula y punto, a diferencia de los errores internos de Go.
type errorDelPedido struct {
	mensaje string
}

func (problema errorDelPedido) Error() string {
	return problema.mensaje
}

// leerCuerpo decodifica el JSON con las mismas reglas que el contrato: tiene que
// ser application/json y no admite campos desconocidos (additionalProperties: false).
func leerCuerpo(contexto *gin.Context) (cuerpoNuevaNotificacion, error) {
	tipo, _, err := mime.ParseMediaType(contexto.GetHeader("Content-Type"))
	if err != nil || tipo != "application/json" {
		return cuerpoNuevaNotificacion{}, errorDelPedido{"El cuerpo tiene que ser JSON (Content-Type: application/json)."}
	}

	contenido, err := io.ReadAll(http.MaxBytesReader(contexto.Writer, contexto.Request.Body, largoMaximoCuerpoHTTP))
	if err != nil {
		return cuerpoNuevaNotificacion{}, errorDelPedido{fmt.Sprintf("El cuerpo supera los %d KiB.", largoMaximoCuerpoHTTP>>10)}
	}
	decodificador := json.NewDecoder(bytes.NewReader(contenido))
	decodificador.DisallowUnknownFields()
	var cuerpo cuerpoNuevaNotificacion
	if err := decodificador.Decode(&cuerpo); err != nil {
		return cuerpoNuevaNotificacion{}, errorDelPedido{fmt.Sprintf("JSON inválido: %v.", err)}
	}
	if decodificador.More() {
		return cuerpoNuevaNotificacion{}, errorDelPedido{"JSON inválido: hay contenido después del objeto."}
	}
	return cuerpo, nil
}

func aRespuesta(notificacion dominio.Notificacion) respuestaNotificacion {
	respuesta := respuestaNotificacion{
		ID:           notificacion.ID,
		Canal:        string(notificacion.Canal),
		Destinatario: notificacion.Destinatario,
		Asunto:       notificacion.Asunto,
		Estado:       string(notificacion.Estado),
		Motivo:       notificacion.Motivo,
		CreadaEn:     notificacion.CreadaEn,
	}
	if !notificacion.EnviadaEn.IsZero() {
		enviadaEn := notificacion.EnviadaEn
		respuesta.EnviadaEn = &enviadaEn
	}
	return respuesta
}

// responderErrorDeDominio traduce los errores del dominio a los códigos del contrato.
// Lo que no es un error conocido es una falla nuestra (por ejemplo, MongoDB caído):
// se registra el detalle y el consumidor recibe 503 para que reintente.
func responderErrorDeDominio(contexto *gin.Context, err error) {
	switch {
	case errors.Is(err, dominio.ErrSolicitudInvalida):
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", err.Error())
	case errors.Is(err, dominio.ErrNoEncontrada):
		responderError(contexto, http.StatusNotFound, "no_encontrada", "Notificación inexistente.")
	case errors.Is(err, dominio.ErrConflictoIdempotencia):
		responderError(contexto, http.StatusConflict, "conflicto_idempotencia", "La clave de idempotencia ya se usó con otro contenido.")
	default:
		slog.ErrorContext(contexto.Request.Context(), "error atendiendo una solicitud de notificaciones",
			"metodo", contexto.Request.Method, "ruta", contexto.FullPath(), "error", err)
		responderError(contexto, http.StatusServiceUnavailable, "no_disponible", "Servicio temporalmente no disponible.")
	}
}

func responderError(contexto *gin.Context, estado int, codigo, mensaje string) {
	contexto.AbortWithStatusJSON(estado, respuestaError{Codigo: codigo, Mensaje: mensaje})
}

// autenticar busca el grupo dueño de la X-API-Key.
func autenticar(clavesAPI map[string]string) gin.HandlerFunc {
	return func(contexto *gin.Context) {
		grupo, existe := clavesAPI[contexto.GetHeader("X-API-Key")]
		if !existe {
			responderError(contexto, http.StatusUnauthorized, "no_autenticado", "Clave de API ausente o inválida.")
			return
		}
		contexto.Set(claveGrupo, grupo)
		contexto.Next()
	}
}

func salud(chequear func(contexto context.Context) error) gin.HandlerFunc {
	return func(contexto *gin.Context) {
		if chequear != nil {
			if err := chequear(contexto.Request.Context()); err != nil {
				slog.WarnContext(contexto.Request.Context(), "chequeo de salud fallido", "error", err)
				contexto.JSON(http.StatusServiceUnavailable, gin.H{"status": "degradado", "service": "notificaciones"})
				return
			}
		}
		contexto.JSON(http.StatusOK, gin.H{"status": "ok", "service": "notificaciones"})
	}
}

// vivo responde si el proceso atiende, sin mirar ninguna dependencia. Lo usa el
// gateway para sacar del balanceo a una instancia caída (ADR-012): si consultara
// MongoDB, como /health, una falla de MongoDB sacaría a todas las instancias juntas.
func vivo(contexto *gin.Context) {
	contexto.JSON(http.StatusOK, gin.H{"status": "vivo", "service": "notificaciones"})
}

// ParsearClavesAPI lee la variable NOTIFICACIONES_CLAVES_API, con el formato
// "grupo-a:clave1,grupo-b:clave2", y devuelve el mapa clave -> grupo.
func ParsearClavesAPI(texto string) (map[string]string, error) {
	claves := map[string]string{}
	for _, par := range strings.Split(texto, ",") {
		par = strings.TrimSpace(par)
		if par == "" {
			continue
		}
		grupo, clave, tieneSeparador := strings.Cut(par, ":")
		grupo, clave = strings.TrimSpace(grupo), strings.TrimSpace(clave)
		if !tieneSeparador || grupo == "" || clave == "" {
			return nil, fmt.Errorf("claves de API: %q no tiene el formato grupo:clave", par)
		}
		if _, repetida := claves[clave]; repetida {
			return nil, fmt.Errorf("claves de API: la clave del grupo %q está repetida", grupo)
		}
		claves[clave] = grupo
	}
	if len(claves) == 0 {
		return nil, errors.New("claves de API: no hay ninguna configurada")
	}
	return claves, nil
}
