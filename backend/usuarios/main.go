package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/handler"
	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/metricas"
	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/registro"
	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/repositorio"
	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/servicio"
	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/trazas"
)

const nombreServicio = "usuarios"

const (
	esperaAlArrancar = 30 * time.Second // para crear la tabla y la cuenta del vendedor
	esperaSalud      = 2 * time.Second
	esperaAlApagar   = 10 * time.Second // para terminar las solicitudes en curso
)

func main() {
	slog.SetDefault(registro.NuevoLogger(os.Stdout, nombreServicio))

	if err := servir(); err != nil {
		slog.Error("usuarios terminó con error", "error", err)
		os.Exit(1)
	}
}

func servir() error {
	configuracion, err := leerConfiguracion(os.Getenv)
	if err != nil {
		return err
	}

	// El contexto se cancela cuando Docker pide apagar el servicio (SIGTERM) o con Ctrl+C.
	contexto, cancelar := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancelar()

	apagarTrazas, err := trazas.Iniciar(contexto, nombreServicio)
	if err != nil {
		return err
	}
	defer func() {
		contextoTrazas, cancelarTrazas := context.WithTimeout(context.Background(), esperaAlApagar)
		defer cancelarTrazas()
		apagarTrazas(contextoTrazas)
	}()

	servicioUsuarios, err := armar(contexto, configuracion)
	if err != nil {
		return err
	}

	medidor := metricas.Nuevas()
	router := gin.New()
	router.Use(otelgin.Middleware(nombreServicio, otelgin.WithFilter(sinChequeosDeSalud)), gin.Recovery(), registro.Solicitudes(slog.Default()), medidor.Solicitudes())
	handler.Registrar(router, servicioUsuarios, esperaSalud)
	servidor := &http.Server{
		Addr:              ":" + configuracion.puerto,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	var procesos sync.WaitGroup
	procesos.Add(1)
	go func() {
		defer procesos.Done()
		if err := medidor.Servir(contexto, configuracion.puertoMetricas); err != nil {
			slog.Error("no se pudieron publicar las métricas", "error", err)
		}
	}()

	errorDelServidor := make(chan error, 1)
	go func() {
		slog.Info("usuarios escuchando", "puerto", configuracion.puerto)
		if err := servidor.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errorDelServidor <- err
		}
		close(errorDelServidor)
	}()

	select {
	case err := <-errorDelServidor:
		cancelar()
		procesos.Wait()
		return fmt.Errorf("servidor HTTP: %w", err)
	case <-contexto.Done():
	}

	slog.Info("apagando usuarios")
	contextoApagado, cancelarApagado := context.WithTimeout(context.Background(), esperaAlApagar)
	defer cancelarApagado()
	if err := servidor.Shutdown(contextoApagado); err != nil {
		return fmt.Errorf("apagar el servidor HTTP: %w", err)
	}
	procesos.Wait()
	return nil
}

// armar conecta las capas, crea la tabla y la cuenta del vendedor.
func armar(contexto context.Context, configuracion configuracion) (*servicio.Servicio, error) {
	repositorioUsuarios, err := repositorio.Abrir(configuracion.dsnMySQL)
	if err != nil {
		return nil, err
	}
	contextoArranque, cancelar := context.WithTimeout(contexto, esperaAlArrancar)
	defer cancelar()
	if err := repositorioUsuarios.Preparar(contextoArranque); err != nil {
		return nil, fmt.Errorf("preparar MySQL: %w", err)
	}
	servicioUsuarios, err := servicio.Nuevo(repositorioUsuarios, []byte(configuracion.secretoJWT), ahora, nuevoUUID)
	if err != nil {
		return nil, err
	}
	if err := servicioUsuarios.AsegurarVendedor(contextoArranque, configuracion.emailVendedor, configuracion.claveVendedor); err != nil {
		return nil, err
	}
	return servicioUsuarios, nil
}

// sinChequeosDeSalud evita trazar /health y /vivo (ADR-006).
func sinChequeosDeSalud(solicitud *http.Request) bool {
	return solicitud.URL.Path != "/health" && solicitud.URL.Path != "/vivo"
}

// ahora es el reloj del servicio, truncado a la precisión con la que MySQL guarda
// las fechas (milisegundos).
func ahora() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}

// nuevoUUID genera un UUID versión 4 (aleatorio).
func nuevoUUID() string {
	var bytes [16]byte
	rand.Read(bytes[:])
	bytes[6] = bytes[6]&0x0f | 0x40 // versión 4
	bytes[8] = bytes[8]&0x3f | 0x80 // variante RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}
