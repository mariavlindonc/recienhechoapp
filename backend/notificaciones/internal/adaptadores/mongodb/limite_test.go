package mongodb_test

// Este test no necesita MongoDB: apunta a un puerto donde no escucha nadie y
// comprueba que cada operación se corta a los pocos segundos y no a los ~30 s que
// espera el driver por defecto.

import (
	"context"
	"net"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/adaptadores/mongodb"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

func TestSinMongoDBCadaOperacionSeCortaAlLimite(prueba *testing.T) {
	// Se reserva un puerto libre y se cierra: queda un puerto sin nadie escuchando.
	oyente, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		prueba.Fatalf("reservar un puerto: %v", err)
	}
	direccion := oyente.Addr().String()
	oyente.Close()

	cliente, err := mongo.Connect(options.Client().ApplyURI("mongodb://" + direccion))
	if err != nil {
		prueba.Fatalf("configurar el cliente: %v", err)
	}
	prueba.Cleanup(func() { cliente.Disconnect(context.Background()) })
	repositorio := mongodb.NuevoRepositorio(cliente.Database("notificaciones_test_sin_mongodb"))

	notificacion := notificacionDePrueba(prueba, "id-1", "grupo-a")
	operaciones := map[string]func(contexto context.Context) error{
		"ReservarClave": func(contexto context.Context) error {
			_, _, err := repositorio.ReservarClave(contexto, puertos.ClaveIdempotencia{Grupo: "grupo-a", Clave: "clave"})
			return err
		},
		"CrearSiNoExiste": func(contexto context.Context) error {
			return repositorio.CrearSiNoExiste(contexto, notificacion)
		},
		"Buscar": func(contexto context.Context) error {
			_, err := repositorio.Buscar(contexto, "grupo-a", "id-1")
			return err
		},
		"TomarPendiente": func(contexto context.Context) error {
			_, _, err := repositorio.TomarPendiente(contexto, momento, momento.Add(time.Minute))
			return err
		},
		"Actualizar": func(contexto context.Context) error {
			return repositorio.Actualizar(contexto, notificacion)
		},
	}

	for nombre, operacion := range operaciones {
		prueba.Run(nombre, func(prueba *testing.T) {
			prueba.Parallel()
			inicio := time.Now()
			err := operacion(context.Background())
			if err == nil {
				prueba.Fatal("esperaba un error con MongoDB caído y no hubo ninguno")
			}
			if duracion := time.Since(inicio); duracion > 5*time.Second {
				prueba.Fatalf("la operación tardó %v: tiene que cortarse a los 2 s", duracion)
			}
		})
	}
}
