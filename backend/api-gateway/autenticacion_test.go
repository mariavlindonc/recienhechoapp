package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const secretoDePrueba = "secreto-de-prueba-de-al-menos-32-bytes"

// firmar arma un token como el de usuarios, con el secreto y el vencimiento dados.
func firmar(prueba *testing.T, secreto string, metodo jwt.SigningMethod, vence time.Time) string {
	prueba.Helper()
	datos := afirmaciones{
		Email: "ana@ejemplo.com",
		Rol:   "comprador",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "id-ana",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(vence),
		},
	}
	if vence.IsZero() {
		datos.ExpiresAt = nil
	}
	token, err := jwt.NewWithClaims(metodo, datos).SignedString([]byte(secreto))
	if err != nil {
		prueba.Fatalf("firmar: %v", err)
	}
	return token
}

// pedirAPedidos manda GET /pedidos con los headers dados y devuelve la respuesta
// del gateway y lo que le llegó a pedidos (nil si no le llegó nada).
func pedirAPedidos(prueba *testing.T, headers map[string]string) (*httptest.ResponseRecorder, *http.Request) {
	prueba.Helper()
	pedidos, recibidas := instanciaFalsa(prueba, "pedidos", 0)
	gin.SetMode(gin.TestMode)
	router := nuevoRouter([]byte(secretoDePrueba), http.NotFoundHandler(), nuevoProxyBalanceado("pedidos", []*url.URL{pedidos}, time.Second, nil), http.NotFoundHandler(), http.NotFoundHandler())

	pedido := httptest.NewRequest(http.MethodGet, "/pedidos", nil)
	for nombre, valor := range headers {
		pedido.Header.Set(nombre, valor)
	}
	respuesta := httptest.NewRecorder()
	router.ServeHTTP(respuesta, pedido)
	if len(*recibidas) == 0 {
		return respuesta, nil
	}
	return respuesta, (*recibidas)[0]
}

func TestTokenValidoEscribeLaIdentidad(prueba *testing.T) {
	token := firmar(prueba, secretoDePrueba, jwt.SigningMethodHS256, time.Now().Add(time.Hour))

	_, recibida := pedirAPedidos(prueba, map[string]string{"Authorization": "Bearer " + token, "X-Id-Usuario": "id-falso", "X-Rol-Usuario": "vendedor"})

	if recibida == nil {
		prueba.Fatal("la solicitud no llegó a pedidos")
	}
	if recibida.Header.Get("X-Id-Usuario") != "id-ana" || recibida.Header.Get("X-Email-Usuario") != "ana@ejemplo.com" || recibida.Header.Get("X-Rol-Usuario") != "comprador" {
		prueba.Errorf("llegó la identidad %v, se esperaba la del token", recibida.Header)
	}
}

func TestSinTokenSeBorraLaIdentidadDelCliente(prueba *testing.T) {
	_, recibida := pedirAPedidos(prueba, map[string]string{"X-Id-Usuario": "id-falso", "X-Email-Usuario": "x@x.com", "X-Rol-Usuario": "vendedor"})

	if recibida == nil {
		prueba.Fatal("sin token la solicitud tiene que seguir: decide el servicio")
	}
	for _, header := range headersDeIdentidad {
		if valor := recibida.Header.Get(header); valor != "" {
			prueba.Errorf("%s = %q, tenía que borrarse", header, valor)
		}
	}
}

func TestTokenInvalidoSeRechaza(prueba *testing.T) {
	vigente := time.Now().Add(time.Hour)
	casos := map[string]struct {
		autorizacion string
		codigo       string
	}{
		"vencido":         {"Bearer " + firmar(prueba, secretoDePrueba, jwt.SigningMethodHS256, time.Now().Add(-time.Minute)), "sesion_vencida"},
		"otro secreto":    {"Bearer " + firmar(prueba, "otro-secreto-de-al-menos-32-bytes!!", jwt.SigningMethodHS256, vigente), "sin_sesion"},
		"otro algoritmo":  {"Bearer " + firmar(prueba, secretoDePrueba, jwt.SigningMethodHS512, vigente), "sin_sesion"},
		"sin Bearer":      {firmar(prueba, secretoDePrueba, jwt.SigningMethodHS256, vigente), "sin_sesion"},
		"cualquier cosa":  {"Bearer abc.def.ghi", "sin_sesion"},
		"sin vencimiento": {"Bearer " + firmar(prueba, secretoDePrueba, jwt.SigningMethodHS256, time.Time{}), "sin_sesion"},
	}

	for nombre, caso := range casos {
		prueba.Run(nombre, func(prueba *testing.T) {
			respuesta, recibida := pedirAPedidos(prueba, map[string]string{"Authorization": caso.autorizacion})

			if respuesta.Code != http.StatusUnauthorized || recibida != nil {
				prueba.Fatalf("estado %d, llegó a pedidos: %v", respuesta.Code, recibida != nil)
			}
			var cuerpo map[string]string
			if err := json.Unmarshal(respuesta.Body.Bytes(), &cuerpo); err != nil || cuerpo["codigo"] != caso.codigo || cuerpo["mensaje"] == "" {
				prueba.Errorf("se esperaba {codigo: %s, mensaje: ...}, llegó %s", caso.codigo, respuesta.Body.String())
			}
		})
	}
}
