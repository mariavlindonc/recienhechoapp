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

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/adaptadores/api"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/adaptadores/eventos"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/adaptadores/inventario"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/adaptadores/metricas"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/adaptadores/mongodb"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/adaptadores/productos"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/adaptadores/rabbitmq"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/aplicacion"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/registro"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/trazas"
)

const nombreServicio = "pedidos"

const (
	esperaAlArrancar = 30 * time.Second // para conectar con MongoDB y crear los índices
	esperaSalud      = 2 * time.Second
	esperaAlApagar   = 10 * time.Second // para terminar las solicitudes en curso
	intervalo        = time.Second      // de los procesos en segundo plano
)

func main() {
	slog.SetDefault(registro.NuevoLogger(os.Stdout, nombreServicio))

	if err := ejecutar(); err != nil {
		slog.Error("el servicio de pedidos terminó con error", "error", err)
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

	clienteMongo, err := mongo.Connect(options.Client().ApplyURI(configuracion.direccionMongo).SetMonitor(trazas.MonitorMongo()))
	if err != nil {
		return fmt.Errorf("configurar la conexión a MongoDB: %w", err)
	}
	defer clienteMongo.Disconnect(context.Background())

	repositorio := mongodb.NuevoRepositorio(clienteMongo, configuracion.baseMongo)
	contextoArranque, cancelarArranque := context.WithTimeout(contexto, esperaAlArrancar)
	defer cancelarArranque()
	if err := repositorio.PrepararIndices(contextoArranque); err != nil {
		return fmt.Errorf("preparar MongoDB: %w", err)
	}

	publicador := rabbitmq.NuevoPublicador(configuracion.direccionRabbit)
	defer publicador.Cerrar()

	medidor := metricas.Nuevas()
	observador := eventos.Nuevos(medidor, slog.Default())
	// shortcut: inventario falso hasta que el grupo proveedor publique su contrato
	// (ADR-010); el adaptador real se enchufa en el mismo lugar, detrás del disyuntor.
	reservador := inventario.NuevoDisyuntor(inventario.NuevoFalso(configuracion.modoInventario), observador, inventario.ConfiguracionDelServicio)
	servicio := aplicacion.NuevoServicio(aplicacion.Dependencias{
		Repositorio: repositorio,
		Bandeja:     repositorio,
		Publicador:  publicador,
		Catalogo:    productos.NuevoCatalogo(configuracion.direccionProductos),
		Inventario:  reservador,
		Eventos:     observador,
		Ahora:       ahora,
		NuevoID:     nuevoUUID,
	})

	router := gin.New()
	router.Use(otelgin.Middleware(nombreServicio, otelgin.WithFilter(sinChequeosDeSalud)), gin.Recovery(), registro.Solicitudes(slog.Default()), medidor.Solicitudes())
	api.Registrar(router, servicio, func(contextoSalud context.Context) error {
		contextoSalud, cancelarSalud := context.WithTimeout(contextoSalud, esperaSalud)
		defer cancelarSalud()
		return clienteMongo.Ping(contextoSalud, nil)
	})
	servidor := &http.Server{
		Addr:              ":" + configuracion.puerto,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	var procesos sync.WaitGroup
	procesos.Add(3)
	go func() {
		defer procesos.Done()
		repetir(contexto, intervalo, "eventos publicados", servicio.PublicarEventos)
	}()
	go func() {
		defer procesos.Done()
		repetir(contexto, intervalo, "pedidos reconfirmados o liberados", servicio.ProcesarConfirmaciones)
	}()
	go func() {
		defer procesos.Done()
		if err := medidor.Servir(contexto, configuracion.puertoMetricas); err != nil {
			slog.Error("no se pudieron publicar las métricas", "error", err)
		}
	}()

	errorDelServidor := make(chan error, 1)
	go func() {
		slog.Info("pedidos escuchando", "puerto", configuracion.puerto, "inventario", configuracion.modoInventario)
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

	slog.Info("apagando pedidos")
	contextoApagado, cancelarApagado := context.WithTimeout(context.Background(), esperaAlApagar)
	defer cancelarApagado()
	if err := servidor.Shutdown(contextoApagado); err != nil {
		return fmt.Errorf("apagar el servidor HTTP: %w", err)
	}
	procesos.Wait()
	return nil
}

// repetir corre un proceso en segundo plano cada intervalo, hasta que se cancela el contexto.
func repetir(contexto context.Context, intervalo time.Duration, descripcion string, proceso func(context.Context) (int, error)) {
	reloj := time.NewTicker(intervalo)
	defer reloj.Stop()
	for {
		cantidad, err := proceso(contexto)
		if err != nil && contexto.Err() == nil {
			slog.Error("proceso en segundo plano con error", "proceso", descripcion, "error", err)
		}
		if cantidad > 0 {
			slog.Info(descripcion, "cantidad", cantidad)
		}
		select {
		case <-contexto.Done():
			return
		case <-reloj.C:
		}
	}
}

// sinChequeosDeSalud evita trazar /health y /vivo (ADR-006).
func sinChequeosDeSalud(pedido *http.Request) bool {
	return pedido.URL.Path != "/health" && pedido.URL.Path != "/vivo"
}

// ahora es el reloj del servicio, truncado a la precisión con la que MongoDB
// guarda las fechas.
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
