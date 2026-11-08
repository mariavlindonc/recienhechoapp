package redis_test

// Tests de integración contra un Redis real, con los mismos casos que el de
// MongoDB. Corren solo si está definida REDIS_URL_TEST, por ejemplo:
//
//	docker run -d --rm --name redis-test -p 6380:6379 redis:7.4.11
//	REDIS_URL_TEST=redis://localhost:6380/0 go test ./...
//
// Cada test vacía esa base al empezar y al terminar: no usar una con datos.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/adaptadores/redis"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

var momento = time.Date(2026, 10, 21, 14, 30, 0, 0, time.UTC)

var contexto = context.Background()

func nuevoRepositorio(prueba *testing.T) (*redis.Repositorio, *goredis.Client) {
	prueba.Helper()
	direccion := os.Getenv("REDIS_URL_TEST")
	if direccion == "" {
		prueba.Skip("REDIS_URL_TEST no está definida: se saltea el test de integración")
	}
	opciones, err := goredis.ParseURL(direccion)
	if err != nil {
		prueba.Fatalf("REDIS_URL_TEST inválida: %v", err)
	}
	cliente := goredis.NewClient(opciones)
	if err := cliente.FlushDB(contexto).Err(); err != nil {
		prueba.Fatalf("Redis no responde en %s: %v", direccion, err)
	}
	repositorio, err := redis.NuevoRepositorio(direccion)
	if err != nil {
		prueba.Fatalf("crear repositorio: %v", err)
	}
	prueba.Cleanup(func() {
		cliente.FlushDB(contexto)
		cliente.Close()
		repositorio.Cerrar()
	})
	return repositorio, cliente
}

func notificacionDePrueba(prueba *testing.T, id, grupo string) dominio.Notificacion {
	prueba.Helper()
	notificacion, err := dominio.NuevaNotificacion(id, grupo, dominio.Solicitud{
		Canal:        dominio.CanalEmail,
		Destinatario: "cliente@ejemplo.com",
		Asunto:       "Tu budín está listo",
		Cuerpo:       "Ya podés retirarlo.",
	}, momento)
	if err != nil {
		prueba.Fatalf("crear notificación de prueba: %v", err)
	}
	return notificacion
}

func TestReservarClave(prueba *testing.T) {
	repositorio, _ := nuevoRepositorio(prueba)
	clave := puertos.ClaveIdempotencia{Grupo: "grupo-a", Clave: "clave-1", Huella: "huella-1", IDNotificacion: "id-1", CreadaEn: momento}

	if _, yaExistia, err := repositorio.ReservarClave(contexto, clave); err != nil || yaExistia {
		prueba.Fatalf("la primera reserva tiene que ser nueva: yaExistia=%v, err=%v", yaExistia, err)
	}

	otroIntento := clave
	otroIntento.Huella, otroIntento.IDNotificacion = "huella-2", "id-2"
	guardada, yaExistia, err := repositorio.ReservarClave(contexto, otroIntento)
	if err != nil || !yaExistia {
		prueba.Fatalf("la segunda reserva tiene que encontrar la primera: yaExistia=%v, err=%v", yaExistia, err)
	}
	if guardada != clave {
		prueba.Errorf("tiene que devolver la clave original sin modificarla, devolvió %+v", guardada)
	}

	deOtroGrupo := clave
	deOtroGrupo.Grupo = "grupo-b"
	if _, yaExistia, err := repositorio.ReservarClave(contexto, deOtroGrupo); err != nil || yaExistia {
		prueba.Errorf("la misma clave en otro grupo es otra reserva: yaExistia=%v, err=%v", yaExistia, err)
	}
}

func TestReservarClaveAlMismoTiempoGanaUnaSola(prueba *testing.T) {
	repositorio, _ := nuevoRepositorio(prueba)
	const intentos = 20
	var nuevas int
	var mutex sync.Mutex
	var grupoDeEspera sync.WaitGroup

	for indice := range intentos {
		grupoDeEspera.Go(func() {
			clave := puertos.ClaveIdempotencia{
				Grupo: "grupo-a", Clave: "clave-1", Huella: "huella",
				IDNotificacion: fmt.Sprintf("id-%d", indice), CreadaEn: momento,
			}
			_, yaExistia, err := repositorio.ReservarClave(contexto, clave)
			if err != nil {
				prueba.Errorf("intento %d: %v", indice, err)
				return
			}
			if !yaExistia {
				mutex.Lock()
				nuevas++
				mutex.Unlock()
			}
		})
	}
	grupoDeEspera.Wait()

	if nuevas != 1 {
		prueba.Errorf("%d reservas quedaron como nuevas, se esperaba 1", nuevas)
	}
}

func TestLasClavesVencenALas24Horas(prueba *testing.T) {
	repositorio, cliente := nuevoRepositorio(prueba)
	repositorio.ReservarClave(contexto, puertos.ClaveIdempotencia{Grupo: "grupo-a", Clave: "clave-1", CreadaEn: momento})

	vigencia, err := cliente.TTL(contexto, "clave:grupo-a:clave-1").Result()
	if err != nil {
		prueba.Fatalf("leer vigencia: %v", err)
	}
	if vigencia <= 23*time.Hour || vigencia > 24*time.Hour {
		prueba.Errorf("la clave vence en %v, se esperaban 24 h", vigencia)
	}
}

func TestCrearYBuscar(prueba *testing.T) {
	repositorio, _ := nuevoRepositorio(prueba)
	original := notificacionDePrueba(prueba, "id-1", "grupo-a")

	if err := repositorio.CrearSiNoExiste(contexto, original); err != nil {
		prueba.Fatalf("crear: %v", err)
	}
	repetida := original
	repetida.Asunto = "No tiene que pisar a la original"
	if err := repositorio.CrearSiNoExiste(contexto, repetida); err != nil {
		prueba.Fatalf("crear con un id repetido no es un error: %v", err)
	}

	encontrada, err := repositorio.Buscar(contexto, "grupo-a", "id-1")
	if err != nil {
		prueba.Fatalf("buscar: %v", err)
	}
	if encontrada != original {
		prueba.Errorf("se guardó distinto:\n tiene %+v\nquería %+v", encontrada, original)
	}

	if _, err := repositorio.Buscar(contexto, "grupo-b", "id-1"); !errors.Is(err, dominio.ErrNoEncontrada) {
		prueba.Errorf("otro grupo no la encuentra; se esperaba ErrNoEncontrada, dio: %v", err)
	}
	if _, err := repositorio.Buscar(contexto, "grupo-a", "no-existe"); !errors.Is(err, dominio.ErrNoEncontrada) {
		prueba.Errorf("se esperaba ErrNoEncontrada, dio: %v", err)
	}
}

func TestTomarPendienteLaApartaYActualizarGuardaElResultado(prueba *testing.T) {
	repositorio, _ := nuevoRepositorio(prueba)
	repositorio.CrearSiNoExiste(contexto, notificacionDePrueba(prueba, "id-1", "grupo-a"))
	hasta := momento.Add(2 * time.Minute)

	tomada, encontrada, err := repositorio.TomarPendiente(contexto, momento, hasta)
	if err != nil || !encontrada || tomada.ID != "id-1" || !tomada.ProximoIntento.Equal(hasta) {
		prueba.Fatalf("se esperaba tomar id-1 apartada hasta %v: %+v, encontrada=%v, err=%v", hasta, tomada, encontrada, err)
	}
	if _, encontrada, _ := repositorio.TomarPendiente(contexto, momento, hasta); encontrada {
		prueba.Error("mientras está apartada, otra instancia no la puede tomar")
	}

	if err := tomada.MarcarEnviada(momento.Add(time.Second)); err != nil {
		prueba.Fatalf("marcar enviada: %v", err)
	}
	if err := repositorio.Actualizar(contexto, tomada); err != nil {
		prueba.Fatalf("actualizar: %v", err)
	}
	guardada, _ := repositorio.Buscar(contexto, "grupo-a", "id-1")
	if guardada.Estado != dominio.EstadoEnviada || !guardada.EnviadaEn.Equal(momento.Add(time.Second)) {
		prueba.Errorf("no se guardó el envío: %+v", guardada)
	}
	if _, encontrada, _ := repositorio.TomarPendiente(contexto, hasta.Add(time.Hour), hasta.Add(2*time.Hour)); encontrada {
		prueba.Error("una enviada no se vuelve a tomar nunca")
	}
}

func TestPendienteApartadaSeRetomaSiVenceElApartado(prueba *testing.T) {
	repositorio, _ := nuevoRepositorio(prueba)
	repositorio.CrearSiNoExiste(contexto, notificacionDePrueba(prueba, "id-1", "grupo-a"))
	hasta := momento.Add(2 * time.Minute)
	repositorio.TomarPendiente(contexto, momento, hasta)

	// La instancia que la tomó se cayó sin terminar: pasado el apartado, se retoma.
	if _, encontrada, _ := repositorio.TomarPendiente(contexto, hasta, hasta.Add(2*time.Minute)); !encontrada {
		prueba.Error("vencido el apartado, otra instancia tiene que poder tomarla")
	}
}

func TestActualizarUnaQueNoExiste(prueba *testing.T) {
	repositorio, _ := nuevoRepositorio(prueba)

	err := repositorio.Actualizar(contexto, notificacionDePrueba(prueba, "no-existe", "grupo-a"))

	if !errors.Is(err, dominio.ErrNoEncontrada) {
		prueba.Errorf("se esperaba ErrNoEncontrada, dio: %v", err)
	}
}
