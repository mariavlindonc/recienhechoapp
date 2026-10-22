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
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/adaptadores/api"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/adaptadores/eventos"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/adaptadores/metricas"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/adaptadores/mongodb"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/adaptadores/smtp"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/aplicacion"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/registro"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/trazas"
)

const nombreServicio = "notificaciones"

const (
	esperaAlArrancar = 30 * time.Second // para conectar con MongoDB y crear los índices
	esperaSalud      = 2 * time.Second
	esperaAlApagar   = 10 * time.Second // para terminar las solicitudes en curso
)

func main() {
	// Todos los logs del servicio (incluidos los de los paquetes internos, que usan
	// slog.Default) salen en JSON con el nombre del servicio (ADR-006).
	slog.SetDefault(registro.NuevoLogger(os.Stdout, nombreServicio))

	if err := ejecutar(); err != nil {
		slog.Error("el servicio de notificaciones terminó con error", "error", err)
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
		// Al apagar, manda los spans que hayan quedado pendientes.
		contextoTrazas, cancelarTrazas := context.WithTimeout(context.Background(), esperaAlApagar)
		defer cancelarTrazas()
		apagarTrazas(contextoTrazas)
	}()

	clienteMongo, err := mongo.Connect(options.Client().ApplyURI(configuracion.direccionMongo).SetMonitor(trazas.MonitorMongo()))
	if err != nil {
		return fmt.Errorf("configurar la conexión a MongoDB: %w", err)
	}
	defer clienteMongo.Disconnect(context.Background())

	repositorio := mongodb.NuevoRepositorio(clienteMongo.Database(configuracion.baseMongo))
	contextoArranque, cancelarArranque := context.WithTimeout(contexto, esperaAlArrancar)
	defer cancelarArranque()
	if err := repositorio.PrepararIndices(contextoArranque); err != nil {
		return fmt.Errorf("preparar MongoDB: %w", err)
	}

	remitente, err := smtp.NuevoRemitente(configuracion.smtp)
	if err != nil {
		return err
	}
	medidor := metricas.Nuevas()
	servicio := aplicacion.NuevoServicio(repositorio, remitente, eventos.Nuevos(medidor, slog.Default()), ahora, nuevoUUID)

	router := gin.New()
	// otelgin va primero: abre el span de la solicitud (o continúa la traza que llega
	// en traceparent) y así el middleware de logs ya ve el trace_id.
	router.Use(otelgin.Middleware(nombreServicio, otelgin.WithFilter(sinChequeosDeSalud)), gin.Recovery(), registro.Solicitudes(slog.Default()), medidor.Solicitudes())
	api.Registrar(router, servicio, api.Configuracion{
		ClavesAPI:       configuracion.clavesAPI,
		LimitePorMinuto: configuracion.limitePorMinuto,
		Ahora:           ahora,
		ChequearSalud: func(contextoSalud context.Context) error {
			contextoSalud, cancelarSalud := context.WithTimeout(contextoSalud, esperaSalud)
			defer cancelarSalud()
			return clienteMongo.Ping(contextoSalud, nil)
		},
	})
	servidor := &http.Server{
		Addr:              ":" + configuracion.puerto,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	var procesos sync.WaitGroup
	procesos.Add(2)
	go func() {
		defer procesos.Done()
		enviarPendientes(contexto, servicio, configuracion.intervaloDeEnvio)
	}()
	go func() {
		defer procesos.Done()
		// Sin métricas el servicio sigue atendiendo: el problema queda en el log.
		if err := medidor.Servir(contexto, configuracion.puertoMetricas); err != nil {
			slog.Error("no se pudieron publicar las métricas", "error", err)
		}
	}()

	errorDelServidor := make(chan error, 1)
	go func() {
		slog.Info("notificaciones escuchando", "puerto", configuracion.puerto)
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

	slog.Info("apagando notificaciones")
	contextoApagado, cancelarApagado := context.WithTimeout(context.Background(), esperaAlApagar)
	defer cancelarApagado()
	if err := servidor.Shutdown(contextoApagado); err != nil {
		return fmt.Errorf("apagar el servidor HTTP: %w", err)
	}
	procesos.Wait()
	return nil
}

// enviarPendientes es el proceso en segundo plano: cada intervalo manda las
// notificaciones pendientes, hasta que se cancela el contexto.
func enviarPendientes(contexto context.Context, servicio *aplicacion.Servicio, intervalo time.Duration) {
	reloj := time.NewTicker(intervalo)
	defer reloj.Stop()
	for {
		procesadas, err := servicio.ProcesarPendientes(contexto)
		if err != nil && contexto.Err() == nil {
			slog.Error("procesando notificaciones pendientes", "error", err)
		}
		if procesadas > 0 {
			slog.Info("notificaciones procesadas", "cantidad", procesadas)
		}
		select {
		case <-contexto.Done():
			return
		case <-reloj.C:
		}
	}
}

// sinChequeosDeSalud evita trazar los chequeos de salud (/health y /vivo): los
// consultan máquinas (el balanceador, un orquestador), cada pocos segundos y
// siempre igual, así que no aportan información y tapan las trazas útiles (ADR-006).
func sinChequeosDeSalud(pedido *http.Request) bool {
	return pedido.URL.Path != "/health" && pedido.URL.Path != "/vivo"
}

// ahora es el reloj del servicio. Se trunca a milisegundos porque es la precisión
// con la que MongoDB guarda las fechas: así una notificación se ve igual recién
// creada que leída de la base.
func ahora() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}

// nuevoUUID genera un UUID versión 4 (aleatorio), el formato de id del contrato.
func nuevoUUID() string {
	var bytes [16]byte
	rand.Read(bytes[:])
	bytes[6] = bytes[6]&0x0f | 0x40 // versión 4
	bytes[8] = bytes[8]&0x3f | 0x80 // variante RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}
