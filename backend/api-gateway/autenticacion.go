package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// headersDeIdentidad son los que leen pedidos, productos y usuarios para saber
// quién hace la solicitud (ADR-016).
var headersDeIdentidad = []string{"X-Id-Usuario", "X-Email-Usuario", "X-Rol-Usuario"}

// afirmaciones son los datos del token que firma usuarios.
type afirmaciones struct {
	Email string `json:"email"`
	Rol   string `json:"rol"`
	jwt.RegisteredClaims
}

// autenticar borra los headers de identidad que mande el cliente y, si la
// solicitud trae "Authorization: Bearer <token>" con un token válido, los vuelve
// a escribir con los datos del token (ADR-016). Sin token la solicitud sigue sin
// identidad: cada servicio decide si la ruta la necesita y responde 401 sin_sesion.
// Un token vencido responde 401 sesion_vencida, así el frontend manda a ingresar.
func autenticar(secreto []byte) gin.HandlerFunc {
	analizador := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	devolverSecreto := func(*jwt.Token) (any, error) { return secreto, nil }

	return func(contexto *gin.Context) {
		for _, header := range headersDeIdentidad {
			contexto.Request.Header.Del(header)
		}
		autorizacion := contexto.GetHeader("Authorization")
		if autorizacion == "" {
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
		case !conBearer || err != nil || leidas.Subject == "" || leidas.Email == "" || leidas.Rol == "":
			rechazar(contexto, "sin_sesion", "La sesión no es válida. Volvé a ingresar.")
			return
		}

		contexto.Request.Header.Set("X-Id-Usuario", leidas.Subject)
		contexto.Request.Header.Set("X-Email-Usuario", leidas.Email)
		contexto.Request.Header.Set("X-Rol-Usuario", leidas.Rol)
		contexto.Next()
	}
}

func rechazar(contexto *gin.Context, codigo, mensaje string) {
	contexto.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"codigo": codigo, "mensaje": mensaje})
}
