package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/modelo"
	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/servicio"
)

// servicioFalso devuelve lo que se le indique, sin base.
type servicioFalso struct {
	usuario modelo.Usuario
	err     error
}

func (falso servicioFalso) Registrar(context.Context, servicio.DatosRegistro) (modelo.Usuario, error) {
	return falso.usuario, falso.err
}

func (falso servicioFalso) IniciarSesion(context.Context, string, string) (servicio.Sesion, error) {
	return servicio.Sesion{Token: "token", VenceEn: time.Now(), Usuario: falso.usuario}, falso.err
}

func (falso servicioFalso) Perfil(context.Context, string) (modelo.Usuario, error) {
	return falso.usuario, falso.err
}

func (falso servicioFalso) Ping(context.Context) error { return falso.err }

func atender(falso servicioFalso, metodo, ruta, cuerpo string, headers map[string]string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Registrar(router, falso, time.Second)
	solicitud := httptest.NewRequest(metodo, ruta, strings.NewReader(cuerpo))
	solicitud.Header.Set("Content-Type", "application/json")
	for nombre, valor := range headers {
		solicitud.Header.Set(nombre, valor)
	}
	respuesta := httptest.NewRecorder()
	router.ServeHTTP(respuesta, solicitud)
	return respuesta
}

var ana = modelo.Usuario{ID: "id-ana", Email: "ana@ejemplo.com", Nombre: "Ana", HashClave: []byte("$2a$10$hash"), Rol: modelo.RolComprador}

func TestRegistrarNoDevuelveLaClave(prueba *testing.T) {
	respuesta := atender(servicioFalso{usuario: ana}, http.MethodPost, "/usuarios", `{"email":"ana@ejemplo.com","nombre":"Ana","clave":"clave-segura"}`, nil)

	if respuesta.Code != http.StatusCreated {
		prueba.Fatalf("estado = %d: %s", respuesta.Code, respuesta.Body)
	}
	cuerpo := respuesta.Body.String()
	if strings.Contains(strings.ToLower(cuerpo), "clave") || strings.Contains(cuerpo, "$2a$") {
		prueba.Errorf("la respuesta expone la contraseña o su hash: %s", cuerpo)
	}
}

func TestErroresDelServicio(prueba *testing.T) {
	casos := []struct {
		nombre string
		err    error
		estado int
		codigo string
	}{
		{"datos inválidos", modelo.ErrSolicitudInvalida, http.StatusBadRequest, "solicitud_invalida"},
		{"email repetido", modelo.ErrEmailRepetido, http.StatusConflict, "email_repetido"},
		{"base caída", errors.New("MySQL no responde"), http.StatusServiceUnavailable, "no_disponible"},
	}
	for _, caso := range casos {
		prueba.Run(caso.nombre, func(prueba *testing.T) {
			respuesta := atender(servicioFalso{err: caso.err}, http.MethodPost, "/usuarios", `{"email":"ana@ejemplo.com","nombre":"Ana","clave":"clave-segura"}`, nil)

			if respuesta.Code != caso.estado || !strings.Contains(respuesta.Body.String(), caso.codigo) {
				prueba.Errorf("respuesta = %d %s", respuesta.Code, respuesta.Body)
			}
		})
	}
}

func TestIniciarSesionConCredencialesInvalidasDa401(prueba *testing.T) {
	respuesta := atender(servicioFalso{err: modelo.ErrCredencialesInvalidas}, http.MethodPost, "/usuarios/sesion", `{"email":"ana@ejemplo.com","clave":"mal"}`, nil)

	if respuesta.Code != http.StatusUnauthorized {
		prueba.Errorf("estado = %d: %s", respuesta.Code, respuesta.Body)
	}
}

func TestIniciarSesionDevuelveElToken(prueba *testing.T) {
	respuesta := atender(servicioFalso{usuario: ana}, http.MethodPost, "/usuarios/sesion", `{"email":"ana@ejemplo.com","clave":"clave-segura"}`, nil)

	if respuesta.Code != http.StatusOK || !strings.Contains(respuesta.Body.String(), `"token":"token"`) {
		prueba.Errorf("respuesta = %d %s", respuesta.Code, respuesta.Body)
	}
}

func TestPerfilSinSesionDa401(prueba *testing.T) {
	respuesta := atender(servicioFalso{usuario: ana}, http.MethodGet, "/usuarios/yo", "", nil)

	if respuesta.Code != http.StatusUnauthorized {
		prueba.Errorf("estado = %d: %s", respuesta.Code, respuesta.Body)
	}
}

func TestPerfilConSesion(prueba *testing.T) {
	respuesta := atender(servicioFalso{usuario: ana}, http.MethodGet, "/usuarios/yo", "", map[string]string{headerIDUsuario: "id-ana"})

	if respuesta.Code != http.StatusOK || !strings.Contains(respuesta.Body.String(), "ana@ejemplo.com") {
		prueba.Errorf("respuesta = %d %s", respuesta.Code, respuesta.Body)
	}
}

func TestSaludDegradadaSinBase(prueba *testing.T) {
	respuesta := atender(servicioFalso{err: errors.New("MySQL no responde")}, http.MethodGet, "/health", "", nil)

	if respuesta.Code != http.StatusServiceUnavailable {
		prueba.Errorf("estado = %d", respuesta.Code)
	}
}
