package rabbitmq

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/dominio"
)

// casosFalsos recuerda las claves usadas, como el repositorio real: la misma
// clave con el mismo contenido no crea otra notificación.
type casosFalsos struct {
	creadas map[string]dominio.Solicitud
	grupo   string
	fallar  error
}

func (casos *casosFalsos) Crear(_ context.Context, grupo, clave string, solicitud dominio.Solicitud) (dominio.Notificacion, error) {
	if casos.fallar != nil {
		return dominio.Notificacion{}, casos.fallar
	}
	if err := solicitud.Validar(); err != nil {
		return dominio.Notificacion{}, err
	}
	casos.grupo = grupo
	if casos.creadas == nil {
		casos.creadas = map[string]dominio.Solicitud{}
	}
	casos.creadas[clave] = solicitud
	return dominio.Notificacion{ID: clave}, nil
}

type receptorFalso struct {
	confirmado, devuelto, reencolado bool
}

func (receptor *receptorFalso) Ack(bool) error { receptor.confirmado = true; return nil }
func (receptor *receptorFalso) Nack(_, reencolar bool) error {
	receptor.devuelto, receptor.reencolado = true, reencolar
	return nil
}

const eventoListo = `{"idEvento":"evento-1","tipo":"pedido.estado_cambiado","version":1,"ocurridoEn":"2026-10-26T15:00:00Z",
	"datos":{"idPedido":"pedido-1","idComprador":"comprador-1","emailComprador":"cliente@ejemplo.com",
	"estadoAnterior":"en_produccion","estadoNuevo":"listo","fecha":"2026-10-30",
	"items":[{"nombreProducto":"Medialuna","cantidad":12},{"nombreProducto":"Torta","cantidad":1}]}}`

func TestEventoRepetidoCreaUnaSolaNotificacion(prueba *testing.T) {
	casos := &casosFalsos{}

	for range 2 {
		if err := Procesar(context.Background(), casos, []byte(eventoListo)); err != nil {
			prueba.Fatalf("error inesperado: %v", err)
		}
	}

	if len(casos.creadas) != 1 || casos.grupo != GrupoPropio {
		prueba.Fatalf("se esperaba una notificación del grupo propio: %v (%s)", casos.creadas, casos.grupo)
	}
	solicitud := casos.creadas["evento-1"]
	if solicitud.Asunto != "Tu pedido está listo" || !strings.Contains(solicitud.Cuerpo, "(12 × Medialuna, 1 × Torta)") || solicitud.Destinatario != "cliente@ejemplo.com" {
		prueba.Errorf("solicitud = %+v", solicitud)
	}
}

func TestMensajesNoProcesablesVanAFallidos(prueba *testing.T) {
	casos := []struct {
		nombre string
		cuerpo string
	}{
		{"JSON inválido", `{"idEvento":`},
		{"tipo desconocido", `{"idEvento":"evento-1","tipo":"pedido.borrado","version":1}`},
		{"versión nueva", strings.Replace(eventoListo, `"version":1`, `"version":2`, 1)},
		{"email inválido", strings.Replace(eventoListo, "cliente@ejemplo.com", "no-es-un-email", 1)},
	}
	for _, caso := range casos {
		prueba.Run(caso.nombre, func(prueba *testing.T) {
			err := Procesar(context.Background(), &casosFalsos{}, []byte(caso.cuerpo))
			receptor := &receptorFalso{}
			responder(context.Background(), receptor, err)
			if !errors.Is(err, errNoProcesable) || !receptor.devuelto || receptor.reencolado {
				prueba.Errorf("se esperaba descartarlo sin reencolar: %v %+v", err, receptor)
			}
		})
	}
}

func TestFallaTransitoriaSeReintenta(prueba *testing.T) {
	err := Procesar(context.Background(), &casosFalsos{fallar: errors.New("MongoDB caído")}, []byte(eventoListo))
	receptor := &receptorFalso{}
	responder(context.Background(), receptor, err)

	if !receptor.reencolado {
		prueba.Errorf("una falla transitoria vuelve a la cola: %+v", receptor)
	}

	receptor = &receptorFalso{}
	responder(context.Background(), receptor, nil)
	if !receptor.confirmado || receptor.devuelto {
		prueba.Errorf("un evento procesado se confirma: %+v", receptor)
	}
}
