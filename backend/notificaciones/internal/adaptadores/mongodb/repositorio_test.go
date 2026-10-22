package mongodb_test

// Tests de integración contra un MongoDB real. Corren solo si está definida
// MONGO_URI_TEST, por ejemplo:
//
//	docker run -d --rm --name mongo-test -p 27018:27017 mongo:8
//	MONGO_URI_TEST=mongodb://localhost:27018 go test ./...
//
// Cada test usa una base nueva y la borra al terminar.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/adaptadores/mongodb"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

// MongoDB guarda las fechas con precisión de milisegundos.
var momento = time.Date(2026, 10, 21, 14, 30, 0, 0, time.UTC)

func nuevoRepositorio(prueba *testing.T) (*mongodb.Repositorio, *mongo.Database) {
	prueba.Helper()
	direccion := os.Getenv("MONGO_URI_TEST")
	if direccion == "" {
		prueba.Skip("MONGO_URI_TEST no está definida: se saltea el test de integración")
	}

	contexto, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelar()
	cliente, err := mongo.Connect(options.Client().ApplyURI(direccion))
	if err != nil {
		prueba.Fatalf("conectar a MongoDB: %v", err)
	}
	if err := cliente.Ping(contexto, nil); err != nil {
		prueba.Fatalf("MongoDB no responde en %s: %v", direccion, err)
	}

	base := cliente.Database(fmt.Sprintf("notificaciones_test_%d", time.Now().UnixNano()))
	prueba.Cleanup(func() {
		contextoLimpieza, cancelarLimpieza := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelarLimpieza()
		base.Drop(contextoLimpieza)
		cliente.Disconnect(contextoLimpieza)
	})

	repositorio := mongodb.NuevoRepositorio(base)
	if err := repositorio.PrepararIndices(contexto); err != nil {
		prueba.Fatalf("preparar índices: %v", err)
	}
	return repositorio, base
}

func notificacionDePrueba(prueba *testing.T, id, grupo string) dominio.Notificacion {
	prueba.Helper()
	notificacion, err := dominio.NuevaNotificacion(id, grupo, dominio.Solicitud{
		Canal:        dominio.CanalEmail,
		Destinatario: "cliente@ejemplo.com",
		Asunto:       "Tu pedido está listo",
		Cuerpo:       "Ya podés retirarlo.",
	}, momento)
	if err != nil {
		prueba.Fatalf("crear notificación de prueba: %v", err)
	}
	return notificacion
}

var contexto = context.Background()

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
	if guardada.Huella != "huella-1" || guardada.IDNotificacion != "id-1" {
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
		grupoDeEspera.Add(1)
		go func() {
			defer grupoDeEspera.Done()
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
		}()
	}
	grupoDeEspera.Wait()

	if nuevas != 1 {
		prueba.Errorf("%d reservas quedaron como nuevas, se esperaba 1", nuevas)
	}
}

func TestLasClavesVencenALas24Horas(prueba *testing.T) {
	_, base := nuevoRepositorio(prueba)

	cursor, err := base.Collection("claves_idempotencia").Indexes().List(contexto)
	if err != nil {
		prueba.Fatalf("listar índices: %v", err)
	}
	var indices []bson.M
	if err := cursor.All(contexto, &indices); err != nil {
		prueba.Fatalf("leer índices: %v", err)
	}
	for _, indice := range indices {
		if indice["name"] == "vencimiento_24h" {
			if segundos := fmt.Sprint(indice["expireAfterSeconds"]); segundos != "86400" {
				prueba.Errorf("el índice vence a los %s segundos, se esperaban 86400", segundos)
			}
			return
		}
	}
	prueba.Error("no existe el índice TTL vencimiento_24h")
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
	if err != nil || !encontrada || tomada.ID != "id-1" {
		prueba.Fatalf("se esperaba tomar id-1: encontrada=%v, err=%v", encontrada, err)
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
