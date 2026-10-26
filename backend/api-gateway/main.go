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
)

const nombreServicio = "api-gateway"

// tiempoMaximoServicios es el timeout del gateway hacia los servicios internos (ADR-003).
const tiempoMaximoServicios = 3 * time.Second

// esperaAlApagar es cuánto se espera, al apagar, para terminar las solicitudes en
// curso y mandar los últimos spans (igual que en notificaciones).
const esperaAlApagar = 10 * time.Second

func main() {
	// Todos los logs del gateway salen en JSON con el nombre del servicio (ADR-006).
	slog.SetDefault(nuevoLogger(os.Stdout, nombreServicio))

	if err := ejecutar(); err != nil {
		slog.Error("el api-gateway terminó con error", "error", err)
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

	apagarTrazas, err := iniciarTrazas(contexto, nombreServicio)
	if err != nil {
		return err
	}
	defer func() {
		// Al apagar, manda los spans que hayan quedado pendientes.
		contextoTrazas, cancelarTrazas := context.WithTimeout(context.Background(), esperaAlApagar)
		defer cancelarTrazas()
		apagarTrazas(contextoTrazas)
	}()

	medidor := nuevasMetricas()
	proxyNotificaciones := nuevoProxyBalanceado("notificaciones", configuracion.instanciasNotificaciones, tiempoMaximoServicios, medidor.contarReenvio("notificaciones"))
	proxyPedidos := nuevoProxyBalanceado("pedidos", configuracion.instanciasPedidos, tiempoMaximoServicios, medidor.contarReenvio("pedidos"))
	proxyProductos := nuevoProxyBalanceado("productos", configuracion.instanciasProductos, tiempoMaximoServicios, medidor.contarReenvio("productos"))
	servidor := &http.Server{
		Addr:              ":" + configuracion.puerto,
		Handler:           nuevoRouter(proxyNotificaciones, proxyPedidos, proxyProductos, medidor.medirSolicitudes()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	var procesos sync.WaitGroup
	procesos.Add(1)
	go func() {
		defer procesos.Done()
		// Sin métricas el gateway sigue atendiendo: el problema queda en el log.
		if err := medidor.servir(contexto, configuracion.puertoMetricas); err != nil {
			slog.Error("no se pudieron publicar las métricas", "error", err)
		}
	}()

	errorDelServidor := make(chan error, 1)
	go func() {
		slog.Info("api-gateway escuchando", "puerto", configuracion.puerto, "instancias_notificaciones", len(configuracion.instanciasNotificaciones))
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

	slog.Info("apagando api-gateway")
	contextoApagado, cancelarApagado := context.WithTimeout(context.Background(), esperaAlApagar)
	defer cancelarApagado()
	if err := servidor.Shutdown(contextoApagado); err != nil {
		return fmt.Errorf("apagar el servidor HTTP: %w", err)
	}
	procesos.Wait()
	return nil
}

// nuevoRouter arma las rutas públicas. Todo lo que empieza con /notificaciones va
// al servicio de notificaciones tal cual llega (headers, cuerpo y respuesta);
// /pedidos y /capacidad van a pedidos, y /productos, /categorias e /imagenes van
// a productos. /internos no se enruta: es solo para llamadas entre servicios.
//
// shortcut: los headers X-Id-Usuario, X-Email-Usuario y X-Rol-Usuario pasan tal
// cual los manda el cliente. Cuando exista la autenticación, el gateway los arma a
// partir del token y descarta los del cliente.
func nuevoRouter(notificaciones, pedidos, productos http.Handler, extras ...gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	// otelgin va primero: abre el span de la solicitud y así el middleware de logs
	// ya ve el trace_id.
	router.Use(otelgin.Middleware(nombreServicio, otelgin.WithFilter(sinChequeosDeSalud)), gin.Recovery(), registrarSolicitudes(slog.Default()))
	router.Use(extras...)
	router.GET("/health", func(contexto *gin.Context) {
		contexto.JSON(http.StatusOK, gin.H{"status": "ok", "service": nombreServicio})
	})
	router.Any("/notificaciones", gin.WrapH(notificaciones))
	router.Any("/notificaciones/*resto", gin.WrapH(notificaciones))
	for _, ruta := range []string{"/pedidos", "/pedidos/*resto", "/capacidad", "/capacidad/*resto"} {
		router.Any(ruta, gin.WrapH(pedidos))
	}
	for _, ruta := range []string{"/productos", "/productos/*resto", "/categorias", "/categorias/*resto", "/imagenes/*resto"} {
		router.Any(ruta, gin.WrapH(productos))
	}
	return router
}

// sinChequeosDeSalud evita trazar los chequeos de salud: los consultan máquinas,
// cada pocos segundos y siempre igual, así que no aportan información y tapan las
// trazas útiles (ADR-006).
func sinChequeosDeSalud(pedido *http.Request) bool {
	return pedido.URL.Path != "/health"
}
