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

// El registro público crea solo compradores: si alguien manda el rol (o cualquier
// otro campo), se rechaza. Es lo que impide registrarse como vendedor.
func TestElRegistroNoAceptaElRol(prueba *testing.T) {
	respuesta := atender(servicioFalso{usuario: ana}, http.MethodPost, "/usuarios", `{"email":"ana@ejemplo.com","nombre":"Ana","clave":"clave-segura","rol":"vendedor"}`, nil)

	if respuesta.Code != http.StatusBadRequest || !strings.Contains(respuesta.Body.String(), "solicitud_invalida") {
		prueba.Errorf("estado %d: %s", respuesta.Code, respuesta.Body)
	}
}

// Solo se lee un objeto JSON: lo que venga después se rechaza, no se ignora.
func TestDatosDespuesDelJSONSeRechazan(prueba *testing.T) {
	for _, sobrante := range []string{` {"rol":"vendedor"}`, `}`} {
		respuesta := atender(servicioFalso{usuario: ana}, http.MethodPost, "/usuarios", `{"email":"ana@ejemplo.com","nombre":"Ana","clave":"clave-segura"}`+sobrante, nil)
		if respuesta.Code != http.StatusBadRequest {
			prueba.Errorf("sobrante %q: estado %d", sobrante, respuesta.Code)
		}
	}
}

// La sesión del navegador va en una cookie que JavaScript no puede leer (ADR-019).
func TestIniciarSesionDejaLaCookie(prueba *testing.T) {
	respuesta := atender(servicioFalso{usuario: ana}, http.MethodPost, "/usuarios/sesion", `{"email":"ana@ejemplo.com","clave":"clave-segura"}`, nil)

	if respuesta.Code != http.StatusOK {
		prueba.Fatalf("estado = %d: %s", respuesta.Code, respuesta.Body)
	}
	cookies := respuesta.Result().Cookies()
	if len(cookies) != 1 {
		prueba.Fatalf("cookies = %v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != "sesion" || cookie.Value != "token" || cookie.Path != "/" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		prueba.Errorf("cookie = %+v", cookie)
	}
	if cookie.MaxAge != 0 || !cookie.Expires.IsZero() {
		prueba.Errorf("la cookie se borra al cerrar el navegador: %+v", cookie)
	}
}

func TestSiNoEntraNoHayCookie(prueba *testing.T) {
	respuesta := atender(servicioFalso{err: modelo.ErrCredencialesInvalidas}, http.MethodPost, "/usuarios/sesion", `{"email":"ana@ejemplo.com","clave":"mal"}`, nil)

	if respuesta.Code != http.StatusUnauthorized || len(respuesta.Result().Cookies()) != 0 {
		prueba.Errorf("estado %d, cookies %v", respuesta.Code, respuesta.Result().Cookies())
	}
}

// JavaScript no puede borrar la cookie: cerrar sesión es una ruta (ADR-019).
func TestCerrarSesionBorraLaCookie(prueba *testing.T) {
	respuesta := atender(servicioFalso{}, http.MethodDelete, "/usuarios/sesion", "", nil)

	if respuesta.Code != http.StatusNoContent {
		prueba.Fatalf("estado = %d", respuesta.Code)
	}
	cookies := respuesta.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "sesion" || cookies[0].Value != "" || cookies[0].MaxAge >= 0 || !cookies[0].HttpOnly || !cookies[0].Secure {
		prueba.Errorf("cookies = %+v", cookies)
	}
}
