package disyuntor_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/adaptadores/disyuntor"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

// remitenteFalso devuelve el error programado y cuenta cuántas veces lo llamaron.
type remitenteFalso struct {
	mutex   sync.Mutex
	err     error
	llamado int
}

func (remitente *remitenteFalso) Enviar(context.Context, puertos.Mensaje) error {
	remitente.mutex.Lock()
	defer remitente.mutex.Unlock()
	remitente.llamado++
	return remitente.err
}

func (remitente *remitenteFalso) programar(err error) {
	remitente.mutex.Lock()
	defer remitente.mutex.Unlock()
	remitente.err = err
}

func (remitente *remitenteFalso) llamadas() int {
	remitente.mutex.Lock()
	defer remitente.mutex.Unlock()
	return remitente.llamado
}

// eventosFalsos anota los cambios de estado del circuito.
type eventosFalsos struct {
	mutex   sync.Mutex
	cambios []string
}

func (eventos *eventosFalsos) EnvioIntentado(context.Context, puertos.IntentoDeEnvio) {}

func (eventos *eventosFalsos) CircuitoDelCorreoCambio(_ context.Context, anterior, nuevo puertos.EstadoCircuito) {
	eventos.mutex.Lock()
	defer eventos.mutex.Unlock()
	eventos.cambios = append(eventos.cambios, string(anterior)+"→"+string(nuevo))
}

func (eventos *eventosFalsos) registrados() []string {
	eventos.mutex.Lock()
	defer eventos.mutex.Unlock()
	return append([]string(nil), eventos.cambios...)
}

const espera = 100 * time.Millisecond

func nuevoEntorno() (*disyuntor.Remitente, *remitenteFalso, *eventosFalsos) {
	interno, eventos := &remitenteFalso{}, &eventosFalsos{}
	return disyuntor.NuevoRemitente(interno, eventos, disyuntor.Configuracion{FallosParaAbrir: 5, EsperaAbierto: espera}), interno, eventos
}

var contexto = context.Background()

func enviar(remitente *disyuntor.Remitente, veces int) error {
	var ultimo error
	for range veces {
		ultimo = remitente.Enviar(contexto, puertos.Mensaje{Destinatario: "cliente@ejemplo.com"})
	}
	return ultimo
}

func TestCincoFallosSeguidosAbrenElCircuito(prueba *testing.T) {
	remitente, interno, eventos := nuevoEntorno()
	interno.programar(errors.New("servidor de correo caído"))

	enviar(remitente, 4)
	if cambios := eventos.registrados(); len(cambios) != 0 {
		prueba.Fatalf("con 4 fallos el circuito sigue cerrado; hubo cambios %v", cambios)
	}
	enviar(remitente, 1)

	if cambios := eventos.registrados(); len(cambios) != 1 || cambios[0] != "cerrado→abierto" {
		prueba.Fatalf("el quinto fallo tenía que abrirlo; cambios = %v", cambios)
	}
	err := enviar(remitente, 3)
	if !errors.Is(err, puertos.ErrCorreoNoDisponible) {
		prueba.Errorf("con el circuito abierto se esperaba ErrCorreoNoDisponible, dio: %v", err)
	}
	if interno.llamadas() != 5 {
		prueba.Errorf("con el circuito abierto no se tiene que intentar enviar: el SMTP se llamó %d veces, se esperaban 5", interno.llamadas())
	}
}

func TestUnEnvioExitosoReiniciaLaCuenta(prueba *testing.T) {
	remitente, interno, eventos := nuevoEntorno()
	interno.programar(errors.New("timeout"))
	enviar(remitente, 4)
	interno.programar(nil)
	enviar(remitente, 1)
	interno.programar(errors.New("timeout"))
	enviar(remitente, 4)

	if cambios := eventos.registrados(); len(cambios) != 0 {
		prueba.Errorf("los fallos tienen que ser seguidos para abrirlo; cambios = %v", cambios)
	}
}

func TestLosRechazosNoAbrenElCircuito(prueba *testing.T) {
	remitente, interno, eventos := nuevoEntorno()
	interno.programar(puertos.ErrEnvioRechazado)

	err := enviar(remitente, 10)

	if !errors.Is(err, puertos.ErrEnvioRechazado) {
		prueba.Errorf("el rechazo tiene que llegar igual a la aplicación; dio: %v", err)
	}
	if cambios := eventos.registrados(); len(cambios) != 0 {
		prueba.Errorf("un rechazo es una respuesta del servidor: no abre el circuito; cambios = %v", cambios)
	}
}

func TestPasadaLaEsperaUnaPruebaExitosaLoCierra(prueba *testing.T) {
	remitente, interno, eventos := nuevoEntorno()
	interno.programar(errors.New("servidor de correo caído"))
	enviar(remitente, 5)

	time.Sleep(espera + 20*time.Millisecond)
	interno.programar(nil)
	if err := enviar(remitente, 1); err != nil {
		prueba.Fatalf("la prueba en semiabierto tenía que pasar y salir bien; dio: %v", err)
	}

	esperados := []string{"cerrado→abierto", "abierto→semiabierto", "semiabierto→cerrado"}
	cambios := eventos.registrados()
	if len(cambios) != len(esperados) {
		prueba.Fatalf("cambios = %v, se esperaba %v", cambios, esperados)
	}
	for indice := range esperados {
		if cambios[indice] != esperados[indice] {
			prueba.Errorf("cambio %d = %s, se esperaba %s", indice+1, cambios[indice], esperados[indice])
		}
	}
}

func TestUnaPruebaFallidaLoVuelveAAbrir(prueba *testing.T) {
	remitente, interno, eventos := nuevoEntorno()
	interno.programar(errors.New("servidor de correo caído"))
	enviar(remitente, 5)

	time.Sleep(espera + 20*time.Millisecond)
	enviar(remitente, 1)

	cambios := eventos.registrados()
	if ultimo := cambios[len(cambios)-1]; ultimo != "semiabierto→abierto" {
		prueba.Errorf("una prueba fallida lo vuelve a abrir; cambios = %v", cambios)
	}
	if err := enviar(remitente, 1); !errors.Is(err, puertos.ErrCorreoNoDisponible) {
		prueba.Errorf("después de volver a abrirse se esperaba ErrCorreoNoDisponible, dio: %v", err)
	}
}

func TestUnEnvioCortadoPorElApagadoNoCuenta(prueba *testing.T) {
	remitente, interno, eventos := nuevoEntorno()
	interno.programar(context.Canceled)

	enviar(remitente, 10)

	if cambios := eventos.registrados(); len(cambios) != 0 {
		prueba.Errorf("los envíos cortados por el apagado no abren el circuito; cambios = %v", cambios)
	}
}
