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

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/adaptadores/api"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/adaptadores/eventos"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/adaptadores/imagenes"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/adaptadores/meilisearch"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/adaptadores/metricas"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/adaptadores/mysql"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/adaptadores/redis"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/aplicacion"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/registro"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/trazas"
)

const nombreServicio = "productos"

const (
	esperaAlArrancar   = 30 * time.Second // para crear las tablas de MySQL
	esperaSalud        = 2 * time.Second
	esperaAlApagar     = 10 * time.Second // para terminar las solicitudes en curso
	intervaloDelIndice = 2 * time.Second  // de la bandeja del índice (ADR-008)
	esperaDelBuscador  = 5 * time.Second  // entre intentos de preparar Meilisearch
	rutaImagenes       = "/imagenes/"
)

func main() {
	slog.SetDefault(registro.NuevoLogger(os.Stdout, nombreServicio))

	ejecutar := servir
	// `productos reindexar` reconstruye el índice de búsqueda desde MySQL (ADR-008).
	if len(os.Args) > 1 && os.Args[1] == "reindexar" {
		ejecutar = reindexar
	}
	if err := ejecutar(); err != nil {
		slog.Error("productos terminó con error", "error", err)
		os.Exit(1)
	}
}

// piezas es el servicio armado con sus adaptadores.
type piezas struct {
	servicio    *aplicacion.Servicio
	repositorio *mysql.Repositorio
	buscador    *meilisearch.Buscador
	imagenes    *imagenes.Disco
	medidor     *metricas.Metricas
}

func armar(contexto context.Context, configuracion configuracion) (piezas, error) {
	base, err := mysql.Abrir(configuracion.dsnMySQL)
	if err != nil {
		return piezas{}, err
	}
	repositorio := mysql.NuevoRepositorio(base, ahora)
	contextoArranque, cancelar := context.WithTimeout(contexto, esperaAlArrancar)
	defer cancelar()
	if err := repositorio.Preparar(contextoArranque); err != nil {
		return piezas{}, fmt.Errorf("preparar MySQL: %w", err)
	}

	var cache puertos.CacheDelCatalogo = redis.Apagada{}
	if configuracion.cacheHabilitada {
		if cache, err = redis.Nueva(configuracion.direccionRedis); err != nil {
			return piezas{}, err
		}
	}
	disco, err := imagenes.NuevoDisco(configuracion.carpetaImagenes, rutaImagenes)
	if err != nil {
		return piezas{}, err
	}

	medidor := metricas.Nuevas()
	buscador := meilisearch.Nuevo(configuracion.direccionMeili, configuracion.claveMeili)
	servicio := aplicacion.NuevoServicio(aplicacion.Dependencias{
		Categorias: repositorio,
		Productos:  repositorio,
		Bandeja:    repositorio,
		Buscador:   buscador,
		Cache:      cache,
		Imagenes:   disco,
		Eventos:    eventos.Nuevos(medidor, slog.Default()),
		Ahora:      ahora,
		NuevoID:    nuevoUUID,
	})
	return piezas{servicio: servicio, repositorio: repositorio, buscador: buscador, imagenes: disco, medidor: medidor}, nil
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

	armado, err := armar(contexto, configuracion)
	if err != nil {
		return err
	}

	router := gin.New()
	router.Use(otelgin.Middleware(nombreServicio, otelgin.WithFilter(sinChequeosDeSalud)), gin.Recovery(), registro.Solicitudes(slog.Default()), armado.medidor.Solicitudes())
	api.Registrar(router, armado.servicio, func(contextoSalud context.Context) error {
		contextoSalud, cancelarSalud := context.WithTimeout(contextoSalud, esperaSalud)
		defer cancelarSalud()
		return armado.repositorio.Ping(contextoSalud)
	}, gin.Dir(armado.imagenes.Directorio(), false))
	servidor := &http.Server{
		Addr:              ":" + configuracion.puerto,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	var procesos sync.WaitGroup
	procesos.Add(2)
	go func() {
		defer procesos.Done()
		// Meilisearch no frena el arranque (ADR-011): mientras no responde, los
		// cambios esperan en la bandeja.
		if !prepararBuscador(contexto, armado.buscador) {
			return
		}
		repetir(contexto, intervaloDelIndice, armado.servicio.SincronizarIndice)
	}()
	go func() {
		defer procesos.Done()
		if err := armado.medidor.Servir(contexto, configuracion.puertoMetricas); err != nil {
			slog.Error("no se pudieron publicar las métricas", "error", err)
		}
	}()

	errorDelServidor := make(chan error, 1)
	go func() {
		slog.Info("productos escuchando", "puerto", configuracion.puerto, "cache", configuracion.cacheHabilitada)
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

func reindexar() error {
	configuracion, err := leerConfiguracion(os.Getenv)
	if err != nil {
		return err
	}
	contexto, cancelar := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancelar()
	armado, err := armar(contexto, configuracion)
	if err != nil {
		return err
	}
	total, err := armado.servicio.Reindexar(contexto)
	if err != nil {
		return err
	}
	slog.Info("índice de búsqueda reconstruido", "productos", total)
	return nil
}

// prepararBuscador reintenta hasta que Meilisearch acepta la configuración del
// índice. Devuelve false si el servicio se apaga antes.
func prepararBuscador(contexto context.Context, buscador *meilisearch.Buscador) bool {
	for {
		err := buscador.Preparar(contexto)
		if err == nil {
			return true
		}
		slog.Warn("Meilisearch no responde; se reintenta", "error", err, "espera", esperaDelBuscador.String())
		select {
		case <-contexto.Done():
			return false
		case <-time.After(esperaDelBuscador):
		}
	}
}

// repetir corre un proceso en segundo plano cada intervalo, hasta que se cancela
// el contexto. Los resultados y errores los avisa el propio proceso por Eventos.
func repetir(contexto context.Context, intervalo time.Duration, proceso func(context.Context) (int, error)) {
	reloj := time.NewTicker(intervalo)
	defer reloj.Stop()
	for {
		proceso(contexto)
		select {
		case <-contexto.Done():
			return
		case <-reloj.C:
		}
	}
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
