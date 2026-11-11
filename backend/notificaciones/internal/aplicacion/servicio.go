// Package aplicacion tiene los casos de uso de notificaciones: crear (con
// idempotencia), consultar y procesar los envíos pendientes. Usa el dominio para
// las reglas y los puertos para guardar y enviar.
package aplicacion

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

// largoMaximoClave es el máximo de la Idempotency-Key según el contrato.
const largoMaximoClave = 100

// esperaSinCorreo es cuánto se posterga una notificación cuando el servidor de
// correo no está disponible: lo mismo que el disyuntor queda abierto (ADR-011).
const esperaSinCorreo = 30 * time.Second

// tiempoApartado es cuánto queda reservada una notificación para la instancia que
// la tomó. Si esa instancia se cae a mitad del envío, pasado este tiempo otra la
// vuelve a tomar.
const tiempoApartado = 2 * time.Minute

// tiempoParaGuardar es cuánto se espera para guardar el resultado de un envío
// cuando llega el apagado del servicio a mitad del envío.
const tiempoParaGuardar = 5 * time.Second

// Servicio reúne los casos de uso.
type Servicio struct {
	repositorio puertos.Repositorio
	remitente   puertos.Remitente
	eventos     puertos.Eventos
	ahora       func() time.Time
	nuevoID     func() string
}

// NuevoServicio arma el servicio. El reloj y el generador de ids se reciben de
// afuera para que los tests puedan controlarlos.
func NuevoServicio(repositorio puertos.Repositorio, remitente puertos.Remitente, eventos puertos.Eventos, ahora func() time.Time, nuevoID func() string) *Servicio {
	return &Servicio{repositorio: repositorio, remitente: remitente, eventos: eventos, ahora: ahora, nuevoID: nuevoID}
}

// Crear registra una notificación pendiente para el grupo. Repetir el pedido con
// la misma clave y el mismo contenido devuelve la notificación ya creada; con
// otro contenido devuelve dominio.ErrConflictoIdempotencia.
func (servicio *Servicio) Crear(contexto context.Context, grupo, clave string, solicitud dominio.Solicitud) (dominio.Notificacion, error) {
	if err := validarClave(clave); err != nil {
		return dominio.Notificacion{}, err
	}
	if err := solicitud.Validar(); err != nil {
		return dominio.Notificacion{}, err
	}

	ahora := servicio.ahora()
	reserva := puertos.ClaveIdempotencia{
		Grupo:          grupo,
		Clave:          clave,
		Huella:         solicitud.Huella(),
		IDNotificacion: servicio.nuevoID(),
		CreadaEn:       ahora,
	}
	guardada, yaExistia, err := servicio.repositorio.ReservarClave(contexto, reserva)
	if err != nil {
		return dominio.Notificacion{}, fmt.Errorf("reservar clave de idempotencia: %w", err)
	}

	if yaExistia {
		if guardada.Huella != reserva.Huella {
			return dominio.Notificacion{}, dominio.ErrConflictoIdempotencia
		}
		existente, err := servicio.repositorio.Buscar(contexto, grupo, guardada.IDNotificacion)
		if err == nil {
			return existente, nil
		}
		if !errors.Is(err, dominio.ErrNoEncontrada) {
			return dominio.Notificacion{}, fmt.Errorf("buscar notificación ya creada: %w", err)
		}
		// La clave quedó reservada pero la notificación no llegó a guardarse (un
		// intento anterior falló a mitad de camino). Se completa ahora con el id
		// que se había reservado.
	}

	notificacion, err := dominio.NuevaNotificacion(guardada.IDNotificacion, grupo, solicitud, ahora)
	if err != nil {
		return dominio.Notificacion{}, err
	}
	if err := servicio.repositorio.CrearSiNoExiste(contexto, notificacion); err != nil {
		return dominio.Notificacion{}, fmt.Errorf("guardar notificación: %w", err)
	}
	return notificacion, nil
}

// Obtener devuelve una notificación del grupo, o dominio.ErrNoEncontrada si no
// existe o es de otro grupo.
func (servicio *Servicio) Obtener(contexto context.Context, grupo, id string) (dominio.Notificacion, error) {
	notificacion, err := servicio.repositorio.Buscar(contexto, grupo, id)
	if err != nil {
		return dominio.Notificacion{}, fmt.Errorf("buscar notificación %s: %w", id, err)
	}
	// El repositorio ya filtra por grupo; se controla de nuevo porque mostrarle a
	// un grupo la notificación de otro sería una filtración de datos.
	if !notificacion.PerteneceA(grupo) {
		return dominio.Notificacion{}, dominio.ErrNoEncontrada
	}
	return notificacion, nil
}

// ProcesarPendientes envía todas las notificaciones listas para enviarse y
// devuelve cuántas procesó. Lo llama periódicamente un proceso en segundo plano.
func (servicio *Servicio) ProcesarPendientes(contexto context.Context) (int, error) {
	procesadas := 0
	for {
		if err := contexto.Err(); err != nil {
			return procesadas, err
		}
		ahora := servicio.ahora()
		notificacion, encontrada, err := servicio.repositorio.TomarPendiente(contexto, ahora, ahora.Add(tiempoApartado))
		if err != nil {
			return procesadas, fmt.Errorf("tomar notificación pendiente: %w", err)
		}
		if !encontrada {
			return procesadas, nil
		}
		seguir, err := servicio.enviar(contexto, &notificacion)
		if err != nil {
			return procesadas, err
		}
		if !seguir {
			// El correo no está disponible: no tiene sentido tomar más pendientes ahora.
			return procesadas, nil
		}
		procesadas++
	}
}

// enviar intenta un envío y guarda el resultado según el tipo de error. Devuelve
// false cuando el servidor de correo no está disponible: la vuelta tiene que cortar.
func (servicio *Servicio) enviar(contexto context.Context, notificacion *dominio.Notificacion) (bool, error) {
	if notificacion.Vencida(servicio.ahora()) {
		return true, servicio.vencer(contexto, notificacion)
	}

	errEnvio := servicio.remitente.Enviar(contexto, puertos.Mensaje{
		Destinatario: notificacion.Destinatario,
		Asunto:       notificacion.Asunto,
		Cuerpo:       notificacion.Cuerpo,
	})

	// Si el apagado cancela el contexto durante el envío, el resultado se guarda
	// igual: si no, el mail que ya salió se vuelve a mandar cuando vence el apartado.
	contexto, cancelar := context.WithTimeout(context.WithoutCancel(contexto), tiempoParaGuardar)
	defer cancelar()

	ahora := servicio.ahora()
	if errors.Is(errEnvio, puertos.ErrCorreoNoDisponible) {
		// No fue un intento: se posterga sin gastar los reintentos de la notificación.
		if err := notificacion.Postergar(ahora.Add(esperaSinCorreo)); err != nil {
			return false, fmt.Errorf("postergar notificación %s: %w", notificacion.ID, err)
		}
		if err := servicio.repositorio.Actualizar(contexto, *notificacion); err != nil {
			return false, fmt.Errorf("actualizar notificación %s: %w", notificacion.ID, err)
		}
		return false, nil
	}

	var err error
	switch {
	case errEnvio == nil:
		err = notificacion.MarcarEnviada(ahora)
	case errors.Is(errEnvio, puertos.ErrEnvioRechazado):
		err = notificacion.RegistrarRechazo(errEnvio.Error())
	default:
		err = notificacion.RegistrarFalloTransitorio(errEnvio.Error(), ahora)
	}
	if err != nil {
		return true, fmt.Errorf("registrar resultado del envío de %s: %w", notificacion.ID, err)
	}

	// Si el mail salió pero esto falla, la notificación se vuelve a tomar cuando
	// vence el apartado y el mail puede salir dos veces: se prefiere duplicar un
	// aviso antes que perderlo (entrega "al menos una vez").
	if err := servicio.repositorio.Actualizar(contexto, *notificacion); err != nil {
		return true, fmt.Errorf("actualizar notificación %s: %w", notificacion.ID, err)
	}
	servicio.eventos.EnvioIntentado(contexto, puertos.IntentoDeEnvio{
		IDNotificacion: notificacion.ID,
		Numero:         notificacion.Intentos,
		Resultado:      resultadoDelEnvio(errEnvio, notificacion.Estado),
		Error:          errEnvio,
	})
	return true, nil
}

// vencer pasa a fallida una notificación que lleva 24 h pendiente (el correo
// estuvo caído todo ese tiempo) y lo avisa como un envío fallido.
func (servicio *Servicio) vencer(contexto context.Context, notificacion *dominio.Notificacion) error {
	if err := notificacion.MarcarVencida(); err != nil {
		return fmt.Errorf("vencer notificación %s: %w", notificacion.ID, err)
	}
	if err := servicio.repositorio.Actualizar(contexto, *notificacion); err != nil {
		return fmt.Errorf("actualizar notificación %s: %w", notificacion.ID, err)
	}
	servicio.eventos.EnvioIntentado(contexto, puertos.IntentoDeEnvio{
		IDNotificacion: notificacion.ID,
		Numero:         notificacion.Intentos,
		Resultado:      puertos.EnvioFallido,
		Error:          errors.New(dominio.MotivoVencida),
	})
	return nil
}

// resultadoDelEnvio resume cómo terminó un intento de envío.
func resultadoDelEnvio(errEnvio error, estado dominio.Estado) puertos.ResultadoEnvio {
	switch {
	case errEnvio == nil:
		return puertos.EnvioExitoso
	case estado == dominio.EstadoFallida:
		return puertos.EnvioFallido
	default:
		return puertos.EnvioReintento
	}
}

func validarClave(clave string) error {
	largo := utf8.RuneCountInString(clave)
	if largo == 0 || largo > largoMaximoClave {
		return fmt.Errorf("%w: la Idempotency-Key tiene que tener entre 1 y %d caracteres", dominio.ErrSolicitudInvalida, largoMaximoClave)
	}
	return nil
}
