package dominio_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/dominio"
)

var momento = time.Date(2026, 10, 21, 14, 30, 0, 0, time.UTC)

func solicitudValida() dominio.Solicitud {
	return dominio.Solicitud{
		Canal:        dominio.CanalEmail,
		Destinatario: "cliente@ejemplo.com",
		Asunto:       "Tu pedido está listo",
		Cuerpo:       "Ya podés retirarlo.",
	}
}

func TestValidarSolicitud(prueba *testing.T) {
	casos := []struct {
		nombre   string
		cambiar  func(solicitud *dominio.Solicitud)
		esValida bool
	}{
		{"solicitud completa", func(solicitud *dominio.Solicitud) {}, true},
		{"canal no soportado", func(solicitud *dominio.Solicitud) { solicitud.Canal = "sms" }, false},
		{"destinatario vacío", func(solicitud *dominio.Solicitud) { solicitud.Destinatario = "" }, false},
		{"destinatario sin arroba", func(solicitud *dominio.Solicitud) { solicitud.Destinatario = "cliente.ejemplo.com" }, false},
		{"destinatario con nombre", func(solicitud *dominio.Solicitud) { solicitud.Destinatario = "Cliente <cliente@ejemplo.com>" }, false},
		{"destinatario con espacios", func(solicitud *dominio.Solicitud) { solicitud.Destinatario = " cliente@ejemplo.com" }, false},
		{"destinatario demasiado largo", func(solicitud *dominio.Solicitud) {
			solicitud.Destinatario = strings.Repeat("a", 243) + "@ejemplo.com" // 255 caracteres
		}, false},
		{"asunto vacío", func(solicitud *dominio.Solicitud) { solicitud.Asunto = "" }, false},
		{"asunto de 150 caracteres con ñ", func(solicitud *dominio.Solicitud) { solicitud.Asunto = strings.Repeat("ñ", 150) }, true},
		{"asunto de 151 caracteres", func(solicitud *dominio.Solicitud) { solicitud.Asunto = strings.Repeat("a", 151) }, false},
		{"cuerpo vacío", func(solicitud *dominio.Solicitud) { solicitud.Cuerpo = "" }, false},
		{"cuerpo de 5000 caracteres", func(solicitud *dominio.Solicitud) { solicitud.Cuerpo = strings.Repeat("a", 5000) }, true},
		{"cuerpo de 5001 caracteres", func(solicitud *dominio.Solicitud) { solicitud.Cuerpo = strings.Repeat("a", 5001) }, false},
	}

	for _, caso := range casos {
		prueba.Run(caso.nombre, func(prueba *testing.T) {
			solicitud := solicitudValida()
			caso.cambiar(&solicitud)

			err := solicitud.Validar()

			if caso.esValida && err != nil {
				prueba.Fatalf("se esperaba válida, dio error: %v", err)
			}
			if !caso.esValida && !errors.Is(err, dominio.ErrSolicitudInvalida) {
				prueba.Fatalf("se esperaba ErrSolicitudInvalida, dio: %v", err)
			}
		})
	}
}

func TestValidarInformaTodosLosProblemas(prueba *testing.T) {
	solicitud := dominio.Solicitud{Canal: "sms"}

	err := solicitud.Validar()

	for _, problema := range []string{"canal", "destinatario", "asunto", "cuerpo"} {
		if !strings.Contains(err.Error(), problema) {
			prueba.Errorf("el error no menciona %q: %v", problema, err)
		}
	}
}

func TestHuella(prueba *testing.T) {
	original := solicitudValida()

	if original.Huella() != solicitudValida().Huella() {
		prueba.Error("el mismo contenido tiene que dar la misma huella")
	}

	otroCuerpo := solicitudValida()
	otroCuerpo.Cuerpo = "Otro texto."
	if original.Huella() == otroCuerpo.Huella() {
		prueba.Error("otro cuerpo tiene que dar otra huella")
	}

	corridoA := solicitudValida()
	corridoA.Asunto, corridoA.Cuerpo = "ab", "c"
	corridoB := solicitudValida()
	corridoB.Asunto, corridoB.Cuerpo = "a", "bc"
	if corridoA.Huella() == corridoB.Huella() {
		prueba.Error("mover texto de un campo a otro tiene que cambiar la huella")
	}
}

func TestNuevaNotificacion(prueba *testing.T) {
	notificacion, err := dominio.NuevaNotificacion("id-1", "grupo-a", solicitudValida(), momento)
	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}

	if notificacion.Estado != dominio.EstadoPendiente {
		prueba.Errorf("estado = %s, se esperaba pendiente", notificacion.Estado)
	}
	if !notificacion.CreadaEn.Equal(momento) || !notificacion.ProximoIntento.Equal(momento) {
		prueba.Error("una notificación nueva se crea y se puede enviar en el momento")
	}
	if notificacion.Intentos != 0 {
		prueba.Errorf("intentos = %d, se esperaba 0", notificacion.Intentos)
	}
}

func TestNuevaNotificacionRechazaDatosInvalidos(prueba *testing.T) {
	invalida := solicitudValida()
	invalida.Asunto = ""
	if _, err := dominio.NuevaNotificacion("id-1", "grupo-a", invalida, momento); !errors.Is(err, dominio.ErrSolicitudInvalida) {
		prueba.Errorf("con asunto vacío se esperaba ErrSolicitudInvalida, dio: %v", err)
	}

	if _, err := dominio.NuevaNotificacion("id-1", "", solicitudValida(), momento); err == nil {
		prueba.Error("sin grupo se esperaba un error")
	}
}

func TestPerteneceA(prueba *testing.T) {
	notificacion, _ := dominio.NuevaNotificacion("id-1", "grupo-a", solicitudValida(), momento)

	if !notificacion.PerteneceA("grupo-a") {
		prueba.Error("tiene que pertenecer al grupo que la creó")
	}
	if notificacion.PerteneceA("grupo-b") {
		prueba.Error("no puede pertenecer a otro grupo")
	}
}

func TestMarcarEnviada(prueba *testing.T) {
	notificacion, _ := dominio.NuevaNotificacion("id-1", "grupo-a", solicitudValida(), momento)
	enviadaEn := momento.Add(4 * time.Second)

	if err := notificacion.MarcarEnviada(enviadaEn); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if notificacion.Estado != dominio.EstadoEnviada || !notificacion.EnviadaEn.Equal(enviadaEn) {
		prueba.Errorf("se esperaba enviada en %v, quedó %s en %v", enviadaEn, notificacion.Estado, notificacion.EnviadaEn)
	}

	if err := notificacion.MarcarEnviada(enviadaEn); !errors.Is(err, dominio.ErrTransicionInvalida) {
		prueba.Errorf("enviar dos veces tiene que dar ErrTransicionInvalida, dio: %v", err)
	}
}

func TestFallosTransitoriosReintentanConEsperaExponencialYDespuesFallan(prueba *testing.T) {
	notificacion, _ := dominio.NuevaNotificacion("id-1", "grupo-a", solicitudValida(), momento)
	esperas := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute}

	for intento, espera := range esperas {
		if err := notificacion.RegistrarFalloTransitorio("servidor caído", momento); err != nil {
			prueba.Fatalf("intento %d: error inesperado: %v", intento+1, err)
		}
		if notificacion.Estado != dominio.EstadoPendiente {
			prueba.Fatalf("intento %d: estado = %s, se esperaba pendiente", intento+1, notificacion.Estado)
		}
		if !notificacion.ProximoIntento.Equal(momento.Add(espera)) {
			prueba.Errorf("intento %d: próximo intento a los %v, se esperaba a los %v",
				intento+1, notificacion.ProximoIntento.Sub(momento), espera)
		}
	}

	// El primer intento más 3 reintentos fallaron: se agotaron.
	if err := notificacion.RegistrarFalloTransitorio("servidor caído", momento); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if notificacion.Estado != dominio.EstadoFallida {
		prueba.Fatalf("estado = %s, se esperaba fallida", notificacion.Estado)
	}
	if !strings.Contains(notificacion.Motivo, "servidor caído") {
		prueba.Errorf("el motivo tiene que explicar la causa: %q", notificacion.Motivo)
	}
	if notificacion.Intentos != dominio.ReintentosMaximos+1 {
		prueba.Errorf("intentos = %d, se esperaban %d", notificacion.Intentos, dominio.ReintentosMaximos+1)
	}
}

func TestRechazoFallaSinReintentar(prueba *testing.T) {
	notificacion, _ := dominio.NuevaNotificacion("id-1", "grupo-a", solicitudValida(), momento)

	if err := notificacion.RegistrarRechazo("destinatario inexistente"); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if notificacion.Estado != dominio.EstadoFallida || notificacion.Motivo != "destinatario inexistente" {
		prueba.Errorf("se esperaba fallida por rechazo, quedó %s (%q)", notificacion.Estado, notificacion.Motivo)
	}

	if err := notificacion.RegistrarFalloTransitorio("otra cosa", momento); !errors.Is(err, dominio.ErrTransicionInvalida) {
		prueba.Errorf("una fallida no se reintenta; se esperaba ErrTransicionInvalida, dio: %v", err)
	}
}

func TestPostergarNoGastaIntentos(prueba *testing.T) {
	notificacion, _ := dominio.NuevaNotificacion("id-1", "grupo-a", solicitudValida(), momento)

	if err := notificacion.Postergar(momento.Add(30 * time.Second)); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}

	if notificacion.Intentos != 0 || notificacion.Estado != dominio.EstadoPendiente {
		prueba.Errorf("postergar no es un intento: quedó %s con %d intentos", notificacion.Estado, notificacion.Intentos)
	}
	if !notificacion.ProximoIntento.Equal(momento.Add(30 * time.Second)) {
		prueba.Errorf("próximo intento = %v", notificacion.ProximoIntento)
	}

	notificacion.MarcarEnviada(momento)
	if err := notificacion.Postergar(momento); !errors.Is(err, dominio.ErrTransicionInvalida) {
		prueba.Errorf("una enviada no se posterga; se esperaba ErrTransicionInvalida, dio: %v", err)
	}
}

func TestVenceALas24Horas(prueba *testing.T) {
	notificacion, _ := dominio.NuevaNotificacion("id-1", "grupo-a", solicitudValida(), momento)

	if notificacion.Vencida(momento.Add(dominio.VidaMaximaPendiente - time.Second)) {
		prueba.Error("antes de las 24 h no está vencida")
	}
	if !notificacion.Vencida(momento.Add(dominio.VidaMaximaPendiente)) {
		prueba.Fatal("a las 24 h está vencida")
	}

	if err := notificacion.MarcarVencida(); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if notificacion.Estado != dominio.EstadoFallida || notificacion.Motivo != dominio.MotivoVencida {
		prueba.Errorf("se esperaba fallida por vencimiento, quedó %s (%q)", notificacion.Estado, notificacion.Motivo)
	}
	if notificacion.Vencida(momento.Add(48 * time.Hour)) {
		prueba.Error("una fallida ya no se considera vencida")
	}
}
