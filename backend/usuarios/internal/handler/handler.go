// Package handler es la capa HTTP de usuarios (ADR-007): lee la solicitud, llama
// al servicio y arma la respuesta. Se llega solo por el api-gateway (ADR-001).
package handler

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
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/modelo"
	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/servicio"
)

const (
	nombreServicio        = "usuarios"
	largoMaximoCuerpoHTTP = 16 << 10
)

// headerIDUsuario lo pone el api-gateway con el id del token y descarta el que
// mande el cliente (ADR-016).
const headerIDUsuario = "X-Id-Usuario"

// Servicio es lo que esta capa necesita de la de abajo. Lo cumple
// *servicio.Servicio; en los tests se usa uno falso.
type Servicio interface {
	Registrar(contexto context.Context, datos servicio.DatosRegistro) (modelo.Usuario, error)
	IniciarSesion(contexto context.Context, email, clave string) (servicio.Sesion, error)
	Perfil(contexto context.Context, id string) (modelo.Usuario, error)
	Ping(contexto context.Context) error
}

// Registrar agrega las rutas al router.
func Registrar(router gin.IRouter, servicioUsuarios Servicio, esperaSalud time.Duration) {
	manejador := &manejador{servicio: servicioUsuarios, esperaSalud: esperaSalud}

	router.GET("/health", manejador.salud)
	router.GET("/vivo", vivo)
	router.POST("/usuarios", manejador.registrar)
	router.POST("/usuarios/sesion", manejador.iniciarSesion)
	router.GET("/usuarios/yo", manejador.perfil)
}

type manejador struct {
	servicio    Servicio
	esperaSalud time.Duration
}

type cuerpoRegistro struct {
	Email  string `json:"email"`
	Nombre string `json:"nombre"`
	Clave  string `json:"clave"`
}

type cuerpoSesion struct {
	Email string `json:"email"`
	Clave string `json:"clave"`
}

// respuestaUsuario no tiene la contraseña ni su hash (RNF8).
type respuestaUsuario struct {
	ID       string    `json:"id"`
	Email    string    `json:"email"`
	Nombre   string    `json:"nombre"`
	Rol      string    `json:"rol"`
	CreadoEn time.Time `json:"creadoEn"`
}

type respuestaSesion struct {
	Token   string           `json:"token"`
	VenceEn time.Time        `json:"venceEn"`
	Usuario respuestaUsuario `json:"usuario"`
}

type respuestaError struct {
	Codigo  string `json:"codigo"`
	Mensaje string `json:"mensaje"`
}

func aRespuesta(usuario modelo.Usuario) respuestaUsuario {
	return respuestaUsuario{ID: usuario.ID, Email: usuario.Email, Nombre: usuario.Nombre, Rol: string(usuario.Rol), CreadoEn: usuario.CreadoEn}
}

func (manejador *manejador) registrar(contexto *gin.Context) {
	var cuerpo cuerpoRegistro
	if !leerCuerpo(contexto, &cuerpo) {
		return
	}
	usuario, err := manejador.servicio.Registrar(contexto.Request.Context(), servicio.DatosRegistro{Email: cuerpo.Email, Nombre: cuerpo.Nombre, Clave: cuerpo.Clave})
	if err != nil {
		responderErrorDelServicio(contexto, err)
		return
	}
	contexto.Header("Location", "/usuarios/yo")
	contexto.JSON(http.StatusCreated, aRespuesta(usuario))
}

func (manejador *manejador) iniciarSesion(contexto *gin.Context) {
	var cuerpo cuerpoSesion
	if !leerCuerpo(contexto, &cuerpo) {
		return
	}
	sesion, err := manejador.servicio.IniciarSesion(contexto.Request.Context(), cuerpo.Email, cuerpo.Clave)
	if err != nil {
		responderErrorDelServicio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, respuestaSesion{Token: sesion.Token, VenceEn: sesion.VenceEn, Usuario: aRespuesta(sesion.Usuario)})
}

func (manejador *manejador) perfil(contexto *gin.Context) {
	id := contexto.GetHeader(headerIDUsuario)
	if id == "" {
		responderError(contexto, http.StatusUnauthorized, "sin_sesion", "Iniciá sesión para ver tu perfil.")
		return
	}
	usuario, err := manejador.servicio.Perfil(contexto.Request.Context(), id)
	if err != nil {
		responderErrorDelServicio(contexto, err)
		return
	}
	contexto.JSON(http.StatusOK, aRespuesta(usuario))
}

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

// responderErrorDelServicio traduce los errores conocidos. Lo demás es una falla
// nuestra (por ejemplo, MySQL caído): se registra el detalle y el cliente recibe 503.
func responderErrorDelServicio(contexto *gin.Context, err error) {
	switch {
	case errors.Is(err, modelo.ErrSolicitudInvalida):
		responderError(contexto, http.StatusBadRequest, "solicitud_invalida", err.Error())
	case errors.Is(err, modelo.ErrEmailRepetido):
		responderError(contexto, http.StatusConflict, "email_repetido", "Ya existe una cuenta con ese email.")
	case errors.Is(err, modelo.ErrCredencialesInvalidas):
		responderError(contexto, http.StatusUnauthorized, "credenciales_invalidas", "Email o contraseña incorrectos.")
	case errors.Is(err, modelo.ErrNoEncontrado):
		responderError(contexto, http.StatusNotFound, "no_encontrado", "Usuario inexistente.")
	default:
		slog.ErrorContext(contexto.Request.Context(), "error al atender la solicitud", "error", err)
		responderError(contexto, http.StatusServiceUnavailable, "no_disponible", "No pudimos atender la solicitud. Probá en un rato.")
	}
}

func responderError(contexto *gin.Context, estado int, codigo, mensaje string) {
	contexto.AbortWithStatusJSON(estado, respuestaError{Codigo: codigo, Mensaje: mensaje})
}

func (manejador *manejador) salud(contexto *gin.Context) {
	contextoSalud, cancelar := context.WithTimeout(contexto.Request.Context(), manejador.esperaSalud)
	defer cancelar()
	if err := manejador.servicio.Ping(contextoSalud); err != nil {
		slog.WarnContext(contexto.Request.Context(), "chequeo de salud fallido", "error", err)
		contexto.JSON(http.StatusServiceUnavailable, gin.H{"status": "degradado", "service": nombreServicio})
		return
	}
	contexto.JSON(http.StatusOK, gin.H{"status": "ok", "service": nombreServicio})
}

// vivo responde si el proceso atiende, sin mirar dependencias (ADR-012).
func vivo(contexto *gin.Context) {
	contexto.JSON(http.StatusOK, gin.H{"status": "vivo", "service": nombreServicio})
}
