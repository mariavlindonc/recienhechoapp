package main

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// limitador cuenta las solicitudes de cada grupo en ventanas fijas (por ejemplo,
// de un minuto). Copiado de notificaciones, que lo tenía antes de pasar al gateway.
type limitador struct {
	mutex   sync.Mutex
	maximo  int
	ventana time.Duration
	ahora   func() time.Time
	cuentas map[string]cuenta
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
