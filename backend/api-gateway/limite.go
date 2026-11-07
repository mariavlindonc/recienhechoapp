package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// limitador cuenta las solicitudes de cada grupo en ventanas fijas (por ejemplo,
// de un minuto). Copiado de notificaciones, que lo tenía antes de pasar al gateway.
type limitador struct {
	mutex   sync.Mutex
	maximo  int
	ventana time.Duration
	ahora   func() time.Time
	cuentas map[string]cuenta
	// ultimaLimpieza es cuándo se borraron las cuentas vencidas por última vez.
	ultimaLimpieza time.Time
}

type cuenta struct {
	inicio time.Time
	usadas int
}

func nuevoLimitador(maximo int, ventana time.Duration, ahora func() time.Time) *limitador {
	return &limitador{maximo: maximo, ventana: ventana, ahora: ahora, cuentas: map[string]cuenta{}}
}

// permitir anota una solicitud del grupo. Si superó el máximo, devuelve false y
// cuánto falta para que empiece la ventana siguiente.
func (limite *limitador) permitir(grupo string) (bool, time.Duration) {
	limite.mutex.Lock()
	defer limite.mutex.Unlock()

	ahora := limite.ahora()
	// Una vez por ventana se borran las cuentas vencidas: con el límite por IP
	// cada dirección nueva ocupa lugar, y el mapa no puede crecer sin fin.
	if ahora.Sub(limite.ultimaLimpieza) >= limite.ventana {
		for clave, vieja := range limite.cuentas {
			if ahora.Sub(vieja.inicio) >= limite.ventana {
				delete(limite.cuentas, clave)
			}
		}
		limite.ultimaLimpieza = ahora
	}
	actual := limite.cuentas[grupo]
	if ahora.Sub(actual.inicio) >= limite.ventana {
		actual = cuenta{inicio: ahora}
	}
	if actual.usadas >= limite.maximo {
		return false, actual.inicio.Add(limite.ventana).Sub(ahora)
	}
	actual.usadas++
	limite.cuentas[grupo] = actual
	return true, 0
}

// limitarPorGrupo aplica el límite de solicitudes por minuto de cada grupo a
// notificaciones (ADR-004). Todas las claves de un grupo suman. Está en el gateway
// porque ve todas las solicitudes antes de repartirlas, así el límite es exacto
// con varias instancias (ADR-012). Una clave desconocida pasa sin contar:
// notificaciones la rechaza con 401.
func limitarPorGrupo(limite *limitador, clavesAPI map[string]string, siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(escritor http.ResponseWriter, pedido *http.Request) {
		if grupo, existe := clavesAPI[pedido.Header.Get("X-API-Key")]; existe {
			if permitido, espera := limite.permitir(grupo); !permitido {
				escritor.Header().Set("Retry-After", strconv.Itoa(max(int(math.Ceil(espera.Seconds())), 1)))
				responderError(escritor, http.StatusTooManyRequests, "limite_superado", fmt.Sprintf("Límite de %d solicitudes por minuto superado.", limite.maximo))
				return
			}
		}
		siguiente.ServeHTTP(escritor, pedido)
	})
}

// limitarIntentos aplica un límite por IP al registro y al inicio de sesión: frena
// a quien prueba contraseñas y protege la CPU, porque cada intento usa bcrypt
// (ADR-019). La IP es la que informa el servidor del frontend o, si no, la de la
// conexión: X-Forwarded-For lo puede inventar cualquiera. Detrás de otro proxy (por
// ejemplo, el de Vite en desarrollo) todos comparten la IP del proxy.
func limitarIntentos(limite *limitador, siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(escritor http.ResponseWriter, pedido *http.Request) {
		// Sin la barra final, para que "/usuarios/sesion/" no saltee el límite.
		ruta := strings.TrimRight(pedido.URL.Path, "/")
		esIntento := pedido.Method == http.MethodPost && (ruta == "/usuarios" || ruta == "/usuarios/sesion")
		if esIntento {
			if permitido, espera := limite.permitir(ipDelCliente(pedido)); !permitido {
				escritor.Header().Set("Retry-After", strconv.Itoa(max(int(math.Ceil(espera.Seconds())), 1)))
				responderError(escritor, http.StatusTooManyRequests, "demasiados_intentos", "Demasiados intentos. Esperá un minuto y volvé a probar.")
				return
			}
		}
		siguiente.ServeHTTP(escritor, pedido)
	})
}

// Headers con los que el servidor del frontend informa la IP del navegador (#95).
const (
	headerIPNavegador   = "X-IP-Navegador"
	headerClaveFrontend = "X-Clave-Frontend"
)

type claveIPNavegador struct{}

// leerIPDelNavegador toma la IP real del navegador. Hace falta porque todos los
// navegadores entran por el servidor del frontend: el gateway los vería con una sola
// IP. Como el gateway publica su puerto, le cree solo a quien manda la clave
// compartida; si no, cuenta la IP de la conexión. Los dos headers se sacan siempre:
// a los servicios no les llegan.
func leerIPDelNavegador(clave string) gin.HandlerFunc {
	return func(contexto *gin.Context) {
		ip := contexto.Request.Header.Get(headerIPNavegador)
		recibida := contexto.Request.Header.Get(headerClaveFrontend)
		contexto.Request.Header.Del(headerIPNavegador)
		contexto.Request.Header.Del(headerClaveFrontend)
		if ip != "" && subtle.ConstantTimeCompare([]byte(recibida), []byte(clave)) == 1 {
			contexto.Request = contexto.Request.WithContext(context.WithValue(contexto.Request.Context(), claveIPNavegador{}, ip))
		}
		contexto.Next()
	}
}

// ipDelCliente devuelve la IP del navegador si la informó el frontend, o la de la conexión.
func ipDelCliente(pedido *http.Request) string {
	if ip, informada := pedido.Context().Value(claveIPNavegador{}).(string); informada {
		return ip
	}
	ip, _, err := net.SplitHostPort(pedido.RemoteAddr)
	if err != nil {
		return pedido.RemoteAddr
	}
	return ip
}
