package aplicacion_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/aplicacion"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

// escenario junta el servicio con sus falsos para poder revisarlos en cada test.
type escenario struct {
	servicio    *aplicacion.Servicio
	repositorio *repositorioEnMemoria
	remitente   *remitenteFalso
	eventos     *eventosFalsos
	reloj       *relojFalso
}

func nuevoEscenario() escenario {
	repositorio := nuevoRepositorioEnMemoria()
	remitente := &remitenteFalso{}
	eventos := &eventosFalsos{}
	reloj := &relojFalso{momento: time.Date(2026, 10, 21, 14, 30, 0, 0, time.UTC)}
	generador := &generadorDeIDs{}
	return escenario{
		servicio:    aplicacion.NuevoServicio(repositorio, remitente, eventos, reloj.ahora, generador.nuevoID),
		repositorio: repositorio,
		remitente:   remitente,
		eventos:     eventos,
		reloj:       reloj,
	}
}

func solicitudValida() dominio.Solicitud {
	return dominio.Solicitud{
		Canal:        dominio.CanalEmail,
		Destinatario: "cliente@ejemplo.com",
		Asunto:       "Tu pedido está listo",
		Cuerpo:       "Ya podés retirarlo.",
	}
}

var contexto = context.Background()

func TestCrearGuardaUnaNotificacionPendiente(prueba *testing.T) {
	entorno := nuevoEscenario()

	notificacion, err := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if notificacion.ID == "" || notificacion.Estado != dominio.EstadoPendiente {
		prueba.Errorf("se esperaba una notificación pendiente con id, quedó %+v", notificacion)
	}
	if entorno.repositorio.cantidad() != 1 {
		prueba.Errorf("hay %d notificaciones guardadas, se esperaba 1", entorno.repositorio.cantidad())
	}
}

func TestCrearConLaMismaClaveYElMismoContenidoDevuelveLaExistente(prueba *testing.T) {
	entorno := nuevoEscenario()
	primera, _ := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())

	reintento, err := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if reintento.ID != primera.ID {
		prueba.Errorf("el reintento devolvió %s, se esperaba %s", reintento.ID, primera.ID)
	}
	if entorno.repositorio.cantidad() != 1 {
		prueba.Errorf("hay %d notificaciones guardadas, se esperaba 1", entorno.repositorio.cantidad())
	}
}

func TestCrearConLaMismaClaveYOtroContenidoEsConflicto(prueba *testing.T) {
	entorno := nuevoEscenario()
	entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())
	otra := solicitudValida()
	otra.Cuerpo = "Otro texto."

	_, err := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", otra)

	if !errors.Is(err, dominio.ErrConflictoIdempotencia) {
		prueba.Errorf("se esperaba ErrConflictoIdempotencia, dio: %v", err)
	}
}

func TestLaMismaClaveEnDistintosGruposNoChoca(prueba *testing.T) {
	entorno := nuevoEscenario()

	deA, _ := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())
	deB, err := entorno.servicio.Crear(contexto, "grupo-b", "clave-1", solicitudValida())

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if deA.ID == deB.ID {
		prueba.Error("cada grupo tiene sus propias claves: tenían que crearse dos notificaciones")
	}
}

func TestCrearRechazaDatosInvalidosSinGuardarNada(prueba *testing.T) {
	ejemplos := []struct {
		nombre    string
		clave     string
		solicitud dominio.Solicitud
	}{
		{"asunto vacío", "clave-1", func() dominio.Solicitud {
			solicitud := solicitudValida()
			solicitud.Asunto = ""
			return solicitud
		}()},
		{"clave vacía", "", solicitudValida()},
		{"clave de 101 caracteres", strings.Repeat("a", 101), solicitudValida()},
	}

	for _, caso := range ejemplos {
		prueba.Run(caso.nombre, func(prueba *testing.T) {
			entorno := nuevoEscenario()

			_, err := entorno.servicio.Crear(contexto, "grupo-a", caso.clave, caso.solicitud)

			if !errors.Is(err, dominio.ErrSolicitudInvalida) {
				prueba.Errorf("se esperaba ErrSolicitudInvalida, dio: %v", err)
			}
			if entorno.repositorio.cantidad() != 0 {
				prueba.Error("con datos inválidos no se tiene que guardar nada")
			}
		})
	}
}

func TestCrearSeRecuperaSiFalloAMitadDeCamino(prueba *testing.T) {
	entorno := nuevoEscenario()
	entorno.repositorio.fallarAlCrear = true

	// Primer intento: la clave se reserva (con id-1) pero la base se cae al guardar.
	if _, err := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida()); !errors.Is(err, errBaseCaida) {
		prueba.Fatalf("se esperaba el error de la base, dio: %v", err)
	}

	// El consumidor reintenta con la misma clave cuando la base vuelve.
	entorno.repositorio.fallarAlCrear = false
	notificacion, err := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())

	if err != nil {
		prueba.Fatalf("el reintento tenía que funcionar, dio: %v", err)
	}
	if notificacion.ID != "id-1" {
		prueba.Errorf("se esperaba el id reservado en el primer intento (id-1), quedó %s", notificacion.ID)
	}
	if entorno.repositorio.cantidad() != 1 {
		prueba.Errorf("hay %d notificaciones guardadas, se esperaba 1", entorno.repositorio.cantidad())
	}
}

func TestCrearAlMismoTiempoConLaMismaClaveCreaUnaSola(prueba *testing.T) {
	entorno := nuevoEscenario()
	const pedidos = 20
	ids := make([]string, pedidos)
	var grupoDeEspera sync.WaitGroup

	for indice := range pedidos {
		grupoDeEspera.Add(1)
		go func() {
			defer grupoDeEspera.Done()
			notificacion, err := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())
			if err != nil {
				prueba.Errorf("pedido %d: error inesperado: %v", indice, err)
			}
			ids[indice] = notificacion.ID
		}()
	}
	grupoDeEspera.Wait()

	for indice, id := range ids {
		if id != ids[0] {
			prueba.Errorf("pedido %d devolvió %s, se esperaba %s", indice, id, ids[0])
		}
	}
	if entorno.repositorio.cantidad() != 1 {
		prueba.Errorf("hay %d notificaciones guardadas, se esperaba 1", entorno.repositorio.cantidad())
	}
}

func TestObtener(prueba *testing.T) {
	entorno := nuevoEscenario()
	creada, _ := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())

	if encontrada, err := entorno.servicio.Obtener(contexto, "grupo-a", creada.ID); err != nil || encontrada.ID != creada.ID {
		prueba.Errorf("el dueño tiene que poder verla: %+v, %v", encontrada, err)
	}
	if _, err := entorno.servicio.Obtener(contexto, "grupo-b", creada.ID); !errors.Is(err, dominio.ErrNoEncontrada) {
		prueba.Errorf("otro grupo no la puede ver; se esperaba ErrNoEncontrada, dio: %v", err)
	}
	if _, err := entorno.servicio.Obtener(contexto, "grupo-a", "no-existe"); !errors.Is(err, dominio.ErrNoEncontrada) {
		prueba.Errorf("se esperaba ErrNoEncontrada, dio: %v", err)
	}
}

func TestProcesarPendientesEnviaYMarcaEnviada(prueba *testing.T) {
	entorno := nuevoEscenario()
	creada, _ := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())

	procesadas, err := entorno.servicio.ProcesarPendientes(contexto)

	if err != nil || procesadas != 1 {
		prueba.Fatalf("se esperaba 1 procesada sin error, dio %d, %v", procesadas, err)
	}
	enviado := entorno.remitente.enviados[0]
	if enviado.Destinatario != "cliente@ejemplo.com" || enviado.Asunto != "Tu pedido está listo" {
		prueba.Errorf("se envió otro mensaje: %+v", enviado)
	}
	guardada, _ := entorno.servicio.Obtener(contexto, "grupo-a", creada.ID)
	if guardada.Estado != dominio.EstadoEnviada {
		prueba.Errorf("estado = %s, se esperaba enviada", guardada.Estado)
	}
}

func TestProcesarPendientesEnviaTodasLasListas(prueba *testing.T) {
	entorno := nuevoEscenario()
	entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())
	entorno.servicio.Crear(contexto, "grupo-b", "clave-1", solicitudValida())

	procesadas, err := entorno.servicio.ProcesarPendientes(contexto)

	if err != nil || procesadas != 2 {
		prueba.Errorf("se esperaban 2 procesadas sin error, dio %d, %v", procesadas, err)
	}
}

func TestFalloTransitorioSeReintentaRecienCuandoPasaLaEspera(prueba *testing.T) {
	entorno := nuevoEscenario()
	entorno.remitente.errores = []error{errors.New("servidor de correo caído")}
	creada, _ := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())

	entorno.servicio.ProcesarPendientes(contexto)
	guardada, _ := entorno.servicio.Obtener(contexto, "grupo-a", creada.ID)
	if guardada.Estado != dominio.EstadoPendiente {
		prueba.Fatalf("después de un fallo transitorio sigue pendiente; quedó %s", guardada.Estado)
	}

	if procesadas, _ := entorno.servicio.ProcesarPendientes(contexto); procesadas != 0 {
		prueba.Errorf("antes de los 30 s no se reintenta; se procesaron %d", procesadas)
	}

	entorno.reloj.adelantar(30 * time.Second)
	if procesadas, _ := entorno.servicio.ProcesarPendientes(contexto); procesadas != 1 {
		prueba.Errorf("a los 30 s se reintenta; se procesaron %d", procesadas)
	}
	guardada, _ = entorno.servicio.Obtener(contexto, "grupo-a", creada.ID)
	if guardada.Estado != dominio.EstadoEnviada {
		prueba.Errorf("el reintento salió bien; estado = %s, se esperaba enviada", guardada.Estado)
	}
}

func TestSinServidorDeCorreoTerminaFallidaDespuesDeLosReintentos(prueba *testing.T) {
	entorno := nuevoEscenario()
	caido := errors.New("servidor de correo caído")
	entorno.remitente.errores = []error{caido, caido, caido, caido}
	creada, _ := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())

	for range dominio.ReintentosMaximos + 1 {
		entorno.servicio.ProcesarPendientes(contexto)
		entorno.reloj.adelantar(10 * time.Minute)
	}

	guardada, _ := entorno.servicio.Obtener(contexto, "grupo-a", creada.ID)
	if guardada.Estado != dominio.EstadoFallida {
		prueba.Errorf("estado = %s, se esperaba fallida", guardada.Estado)
	}
	if len(entorno.remitente.enviados) != dominio.ReintentosMaximos+1 {
		prueba.Errorf("se intentó %d veces, se esperaban %d", len(entorno.remitente.enviados), dominio.ReintentosMaximos+1)
	}
}

func TestRechazoDelDestinatarioFallaSinReintentar(prueba *testing.T) {
	entorno := nuevoEscenario()
	entorno.remitente.errores = []error{puertos.ErrEnvioRechazado}
	creada, _ := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())

	entorno.servicio.ProcesarPendientes(contexto)
	entorno.reloj.adelantar(10 * time.Minute)
	entorno.servicio.ProcesarPendientes(contexto)

	guardada, _ := entorno.servicio.Obtener(contexto, "grupo-a", creada.ID)
	if guardada.Estado != dominio.EstadoFallida {
		prueba.Errorf("estado = %s, se esperaba fallida", guardada.Estado)
	}
	if len(entorno.remitente.enviados) != 1 {
		prueba.Errorf("se intentó %d veces, se esperaba 1", len(entorno.remitente.enviados))
	}
}

func TestElEnvioFallidoSeAvisaComoEvento(prueba *testing.T) {
	entorno := nuevoEscenario()
	caido := errors.New("servidor de correo caído")
	entorno.remitente.errores = []error{caido}
	creada, _ := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())

	entorno.servicio.ProcesarPendientes(contexto)

	if len(entorno.eventos.intentos) != 1 {
		prueba.Fatalf("se esperaba 1 evento, hubo %d", len(entorno.eventos.intentos))
	}
	intento := entorno.eventos.intentos[0]
	if intento.IDNotificacion != creada.ID || intento.Numero != 1 || intento.Resultado != puertos.EnvioReintento || !errors.Is(intento.Error, caido) {
		prueba.Errorf("evento = %+v", intento)
	}
}

func TestCadaIntentoDeEnvioSeAvisaConSuResultado(prueba *testing.T) {
	caido := errors.New("servidor de correo caído")
	casos := []struct {
		nombre   string
		errores  []error
		esperado map[puertos.ResultadoEnvio]int
	}{
		{"sale bien", nil, map[puertos.ResultadoEnvio]int{puertos.EnvioExitoso: 1}},
		{"rechazada", []error{puertos.ErrEnvioRechazado}, map[puertos.ResultadoEnvio]int{puertos.EnvioFallido: 1}},
		{"se agotan los reintentos", []error{caido, caido, caido, caido}, map[puertos.ResultadoEnvio]int{puertos.EnvioReintento: 3, puertos.EnvioFallido: 1}},
		{"sale en el segundo intento", []error{caido}, map[puertos.ResultadoEnvio]int{puertos.EnvioReintento: 1, puertos.EnvioExitoso: 1}},
	}

	for _, caso := range casos {
		prueba.Run(caso.nombre, func(prueba *testing.T) {
			entorno := nuevoEscenario()
			entorno.remitente.errores = caso.errores
			entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())

			for range dominio.ReintentosMaximos + 1 {
				entorno.servicio.ProcesarPendientes(contexto)
				entorno.reloj.adelantar(10 * time.Minute)
			}

			cuenta := map[puertos.ResultadoEnvio]int{}
			for _, intento := range entorno.eventos.intentos {
				cuenta[intento.Resultado]++
			}
			if len(cuenta) != len(caso.esperado) {
				prueba.Errorf("resultados = %v, se esperaba %v", cuenta, caso.esperado)
			}
			for resultado, cantidad := range caso.esperado {
				if cuenta[resultado] != cantidad {
					prueba.Errorf("%s: %d, se esperaban %d", resultado, cuenta[resultado], cantidad)
				}
			}
		})
	}
}

func TestSinCorreoSePostergaSinGastarIntentosYCortaLaVuelta(prueba *testing.T) {
	entorno := nuevoEscenario()
	entorno.remitente.errores = []error{puertos.ErrCorreoNoDisponible}
	primera, _ := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())
	entorno.servicio.Crear(contexto, "grupo-b", "clave-1", solicitudValida())

	procesadas, err := entorno.servicio.ProcesarPendientes(contexto)

	if err != nil || procesadas != 0 {
		prueba.Fatalf("se esperaban 0 procesadas sin error, dio %d, %v", procesadas, err)
	}
	if len(entorno.remitente.enviados) != 1 {
		prueba.Errorf("con el correo no disponible la vuelta tiene que cortar: se intentaron %d envíos", len(entorno.remitente.enviados))
	}
	guardada, _ := entorno.servicio.Obtener(contexto, "grupo-a", primera.ID)
	if guardada.Intentos != 0 || guardada.Estado != dominio.EstadoPendiente {
		prueba.Errorf("no tiene que gastar un intento: quedó %s con %d intentos", guardada.Estado, guardada.Intentos)
	}
	if !guardada.ProximoIntento.Equal(entorno.reloj.ahora().Add(30 * time.Second)) {
		prueba.Errorf("se esperaba postergada 30 s, quedó para %v", guardada.ProximoIntento)
	}
	if len(entorno.eventos.intentos) != 0 {
		prueba.Errorf("postergar no es un intento de envío: hubo %d eventos", len(entorno.eventos.intentos))
	}
}

func TestUnaPendienteDe24HorasPasaAFallida(prueba *testing.T) {
	entorno := nuevoEscenario()
	creada, _ := entorno.servicio.Crear(contexto, "grupo-a", "clave-1", solicitudValida())
	entorno.reloj.adelantar(dominio.VidaMaximaPendiente)

	entorno.servicio.ProcesarPendientes(contexto)

	guardada, _ := entorno.servicio.Obtener(contexto, "grupo-a", creada.ID)
	if guardada.Estado != dominio.EstadoFallida || guardada.Motivo != dominio.MotivoVencida {
		prueba.Errorf("se esperaba fallida por vencimiento, quedó %s (%q)", guardada.Estado, guardada.Motivo)
	}
	if len(entorno.remitente.enviados) != 0 {
		prueba.Error("una notificación vencida no se intenta enviar")
	}
	if len(entorno.eventos.intentos) != 1 || entorno.eventos.intentos[0].Resultado != puertos.EnvioFallido {
		prueba.Errorf("el vencimiento se avisa como un envío fallido: %+v", entorno.eventos.intentos)
	}
}
