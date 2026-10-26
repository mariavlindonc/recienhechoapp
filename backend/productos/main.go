package main

import (
	"context"
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

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/adaptadores/api"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/adaptadores/metricas"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/registro"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/trazas"
)

const nombreServicio = "productos"

const esperaAlApagar = 10 * time.Second // para terminar las solicitudes en curso

func main() {
	slog.SetDefault(registro.NuevoLogger(os.Stdout, nombreServicio))

	if err := ejecutar(); err != nil {
		slog.Error("el servicio de productos terminó con error", "error", err)
		os.Exit(1)
	}
}

func ejecutar() error {
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

	medidor := metricas.Nuevas()

	router := gin.New()
	router.Use(otelgin.Middleware(nombreServicio, otelgin.WithFilter(sinChequeosDeSalud)), gin.Recovery(), registro.Solicitudes(slog.Default()), medidor.Solicitudes())
	api.Registrar(router, nil)
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
		slog.Info("productos escuchando", "puerto", configuracion.puerto)
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

	slog.Info("apagando productos")
	contextoApagado, cancelarApagado := context.WithTimeout(context.Background(), esperaAlApagar)
	defer cancelarApagado()
	if err := servidor.Shutdown(contextoApagado); err != nil {
		return fmt.Errorf("apagar el servidor HTTP: %w", err)
	}
	procesos.Wait()
	return nil
}

// sinChequeosDeSalud evita trazar /health y /vivo (ADR-006).
func sinChequeosDeSalud(solicitud *http.Request) bool {
	return solicitud.URL.Path != "/health" && solicitud.URL.Path != "/vivo"
}
