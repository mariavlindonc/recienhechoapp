package api

import (
	"sync"
	"time"
)

// limitador cuenta las solicitudes de cada grupo en ventanas fijas (por ejemplo,
// de un minuto). Vive en la memoria de la instancia: con dos instancias, cada una
// cuenta por separado (limitación aceptada, ver ADR-004).
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
