// Package disyuntor protege al servidor de correo con un circuit breaker
// (sony/gobreaker, ADR-011). Envuelve a otro Remitente: mientras el correo está
// caído no intenta enviar y devuelve puertos.ErrCorreoNoDisponible, así la
// aplicación posterga las notificaciones sin gastar sus reintentos.
package disyuntor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sony/gobreaker/v2"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

// Configuracion del disyuntor. Los valores del servicio están en ADR-011.
type Configuracion struct {
	// FallosParaAbrir es cuántos fallos transitorios seguidos abren el circuito.
	FallosParaAbrir uint32
	// EsperaAbierto es cuánto queda abierto antes de dejar pasar un envío de prueba.
	EsperaAbierto time.Duration
}

// ConfiguracionDelServicio son los valores decididos en ADR-011: 5 fallos seguidos
// lo abren por 30 s.
var ConfiguracionDelServicio = Configuracion{FallosParaAbrir: 5, EsperaAbierto: 30 * time.Second}

// Remitente cumple puertos.Remitente envolviendo a otro.
type Remitente struct {
	interno  puertos.Remitente
	circuito *gobreaker.CircuitBreaker[struct{}]
}

var _ puertos.Remitente = (*Remitente)(nil)

// NuevoRemitente envuelve a interno. Cada cambio de estado del circuito se avisa
// por el puerto Eventos.
func NuevoRemitente(interno puertos.Remitente, eventos puertos.Eventos, configuracion Configuracion) *Remitente {
	circuito := gobreaker.NewCircuitBreaker[struct{}](gobreaker.Settings{
		Name: "servidor de correo",
		// En semiabierto pasa un solo envío de prueba: si sale, se cierra.
		MaxRequests: 1,
		Timeout:     configuracion.EsperaAbierto,
		ReadyToTrip: func(cuentas gobreaker.Counts) bool {
			return cuentas.ConsecutiveFailures >= configuracion.FallosParaAbrir
		},
		// Un rechazo definitivo (5xx al destinatario) no es una falla del servidor:
		// el servidor respondió. No abre el circuito.
		IsSuccessful: func(err error) bool {
			return err == nil || errors.Is(err, puertos.ErrEnvioRechazado)
		},
		// Un envío cortado porque el servicio se está apagando no dice nada del
		// servidor de correo: no cuenta ni a favor ni en contra.
		IsExcluded: func(err error) bool {
			return errors.Is(err, context.Canceled)
		},
		OnStateChange: func(_ string, anterior, nuevo gobreaker.State) {
			eventos.CircuitoDelCorreoCambio(context.Background(), aEstado(anterior), aEstado(nuevo))
		},
	})
	return &Remitente{interno: interno, circuito: circuito}
}

// Enviar pasa el mensaje al remitente interno si el circuito lo permite.
func (remitente *Remitente) Enviar(contexto context.Context, mensaje puertos.Mensaje) error {
	_, err := remitente.circuito.Execute(func() (struct{}, error) {
		return struct{}{}, remitente.interno.Enviar(contexto, mensaje)
	})
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		return fmt.Errorf("%w: el disyuntor está %s", puertos.ErrCorreoNoDisponible, aEstado(remitente.circuito.State()))
	}
	return err
}

func aEstado(estado gobreaker.State) puertos.EstadoCircuito {
	switch estado {
	case gobreaker.StateOpen:
		return puertos.CircuitoAbierto
	case gobreaker.StateHalfOpen:
		return puertos.CircuitoSemiabierto
	default:
		return puertos.CircuitoCerrado
	}
}
