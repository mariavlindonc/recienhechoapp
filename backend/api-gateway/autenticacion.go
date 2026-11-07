package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// headersDeIdentidad son los que leen pedidos, productos y usuarios para saber
// quién hace la solicitud (ADR-019).
var headersDeIdentidad = []string{"X-Id-Usuario", "X-Email-Usuario", "X-Rol-Usuario"}

// rutaTiempoReal es el WebSocket con el que el comprador sigue sus pedidos (ADR-018).
const rutaTiempoReal = "/pedidos/tiempo-real"

// afirmaciones son los datos del token que firma usuarios.
type afirmaciones struct {
	Email string `json:"email"`
	Rol   string `json:"rol"`
	jwt.RegisteredClaims
}

// nombreCookieDeSesion es la cookie donde usuarios deja el token del navegador (ADR-019).
const nombreCookieDeSesion = "sesion"

// autenticar borra los headers de identidad que mande el cliente y, si la solicitud
// trae un token válido, los vuelve a escribir con sus datos (ADR-019). El token viene
// en la cookie de sesión (navegador) o en "Authorization: Bearer <token>" (k6 y otros
// clientes, que tiene prioridad). La cookie no sigue viaje: los servicios reciben la
// identidad, no la sesión.
//
// Sin token la solicitud sigue sin identidad: cada servicio decide si la ruta la
// necesita y responde 401 sin_sesion. Con Authorization, un token vencido responde
// 401 sesion_vencida. Una cookie que no sirve se trata como si no estuviera: el POST
// de inicio de sesión también la lleva, y rechazarla impediría volver a entrar.
func autenticar(secreto []byte) gin.HandlerFunc {
	analizador := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	devolverSecreto := func(*jwt.Token) (any, error) { return secreto, nil }

	return func(contexto *gin.Context) {
		for _, header := range headersDeIdentidad {
			contexto.Request.Header.Del(header)
		}
		cookie, errCookie := contexto.Cookie(nombreCookieDeSesion)
		contexto.Request.Header.Del("Cookie")

		autorizacion := contexto.GetHeader("Authorization")
		if autorizacion == "" {
			var leidas afirmaciones
			if errCookie == nil {
				if _, err := analizador.ParseWithClaims(cookie, &leidas, devolverSecreto); err == nil && leidas.completas() {
					escribirIdentidad(contexto, leidas)
				}
			}
			contexto.Next()
			return
		}

		token, conBearer := strings.CutPrefix(autorizacion, "Bearer ")
		var leidas afirmaciones
		var err error
		if conBearer {
			_, err = analizador.ParseWithClaims(token, &leidas, devolverSecreto)
		}
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			rechazar(contexto, "sesion_vencida", "La sesión venció. Volvé a ingresar.")
			return
		case !conBearer || err != nil || !leidas.completas():
			rechazar(contexto, "sin_sesion", "La sesión no es válida. Volvé a ingresar.")
			return
		}

		escribirIdentidad(contexto, leidas)
		contexto.Next()
	}
}

func (leidas afirmaciones) completas() bool {
	return leidas.Subject != "" && leidas.Email != "" && leidas.Rol != ""
}

func escribirIdentidad(contexto *gin.Context, leidas afirmaciones) {
	contexto.Request.Header.Set("X-Id-Usuario", leidas.Subject)
	contexto.Request.Header.Set("X-Email-Usuario", leidas.Email)
	contexto.Request.Header.Set("X-Rol-Usuario", leidas.Rol)
}

func rechazar(contexto *gin.Context, codigo, mensaje string) {
	contexto.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"codigo": codigo, "mensaje": mensaje})
}
