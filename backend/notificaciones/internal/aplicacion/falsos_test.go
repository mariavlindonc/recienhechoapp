package aplicacion_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

// Implementaciones falsas de los puertos, en memoria, para probar los casos de
// uso sin MongoDB ni servidor de correo.

var errBaseCaida = errors.New("base de datos caída")

// repositorioEnMemoria cumple puertos.Repositorio. El mutex hace que cada
// operación sea atómica, como lo es en MongoDB.
type repositorioEnMemoria struct {
	mutex          sync.Mutex
	claves         map[string]puertos.ClaveIdempotencia
	notificaciones map[string]dominio.Notificacion
	orden          []string // ids en orden de creación, para que TomarPendiente sea predecible
	fallarAlCrear  bool     // simula que la base se cae justo al guardar la notificación
}

func nuevoRepositorioEnMemoria() *repositorioEnMemoria {
	return &repositorioEnMemoria{
		claves:         map[string]puertos.ClaveIdempotencia{},
		notificaciones: map[string]dominio.Notificacion{},
	}
}

func (repositorio *repositorioEnMemoria) ReservarClave(_ context.Context, clave puertos.ClaveIdempotencia) (puertos.ClaveIdempotencia, bool, error) {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	llave := clave.Grupo + "\x00" + clave.Clave
	if guardada, existe := repositorio.claves[llave]; existe {
		return guardada, true, nil
	}
	repositorio.claves[llave] = clave
	return clave, false, nil
}

func (repositorio *repositorioEnMemoria) CrearSiNoExiste(_ context.Context, notificacion dominio.Notificacion) error {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	if repositorio.fallarAlCrear {
		return errBaseCaida
	}
	if _, existe := repositorio.notificaciones[notificacion.ID]; existe {
		return nil
	}
	repositorio.notificaciones[notificacion.ID] = notificacion
	repositorio.orden = append(repositorio.orden, notificacion.ID)
	return nil
}

func (repositorio *repositorioEnMemoria) Buscar(_ context.Context, grupo, id string) (dominio.Notificacion, error) {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	notificacion, existe := repositorio.notificaciones[id]
	if !existe || notificacion.Grupo != grupo {
		return dominio.Notificacion{}, dominio.ErrNoEncontrada
	}
	return notificacion, nil
}

func (repositorio *repositorioEnMemoria) TomarPendiente(_ context.Context, ahora, hasta time.Time) (dominio.Notificacion, bool, error) {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	for _, id := range repositorio.orden {
		notificacion := repositorio.notificaciones[id]
		if notificacion.Estado == dominio.EstadoPendiente && !notificacion.ProximoIntento.After(ahora) {
			notificacion.ProximoIntento = hasta
			repositorio.notificaciones[id] = notificacion
			return notificacion, true, nil
		}
	}
	return dominio.Notificacion{}, false, nil
}

func (repositorio *repositorioEnMemoria) Actualizar(_ context.Context, notificacion dominio.Notificacion) error {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	if _, existe := repositorio.notificaciones[notificacion.ID]; !existe {
		return dominio.ErrNoEncontrada
	}
	repositorio.notificaciones[notificacion.ID] = notificacion
	return nil
}

func (repositorio *repositorioEnMemoria) cantidad() int {
	repositorio.mutex.Lock()
	defer repositorio.mutex.Unlock()
	return len(repositorio.notificaciones)
}

// remitenteFalso cumple puertos.Remitente: anota lo que le piden enviar y
// devuelve, en orden, los errores programados (nil cuando se acaban).
type remitenteFalso struct {
	enviados []puertos.Mensaje
	errores  []error
}

func (remitente *remitenteFalso) Enviar(_ context.Context, mensaje puertos.Mensaje) error {
	remitente.enviados = append(remitente.enviados, mensaje)
	if len(remitente.errores) == 0 {
		return nil
	}
	siguiente := remitente.errores[0]
	remitente.errores = remitente.errores[1:]
	return siguiente
}

// relojFalso permite adelantar el tiempo en los tests.
type relojFalso struct {
	momento time.Time
}

func (reloj *relojFalso) ahora() time.Time { return reloj.momento }

func (reloj *relojFalso) adelantar(duracion time.Duration) {
	reloj.momento = reloj.momento.Add(duracion)
}

// generadorDeIDs da "id-1", "id-2", ... de forma segura entre goroutines.
type generadorDeIDs struct {
	mutex    sync.Mutex
	contador int
}

func (generador *generadorDeIDs) nuevoID() string {
	generador.mutex.Lock()
	defer generador.mutex.Unlock()
	generador.contador++
	return fmt.Sprintf("id-%d", generador.contador)
}

// eventosFalsos cumple puertos.Eventos: anota los intentos de envío avisados.
type eventosFalsos struct {
	intentos []puertos.IntentoDeEnvio
}

func (eventos *eventosFalsos) EnvioIntentado(_ context.Context, intento puertos.IntentoDeEnvio) {
	eventos.intentos = append(eventos.intentos, intento)
}
