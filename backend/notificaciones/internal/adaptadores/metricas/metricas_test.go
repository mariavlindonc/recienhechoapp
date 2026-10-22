package metricas_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/adaptadores/metricas"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

// leerMetricas levanta el servidor de métricas en un puerto libre y devuelve lo
// que publica en /metrics.
func leerMetricas(prueba *testing.T, metricas *metricas.Metricas) string {
	prueba.Helper()
	escucha, _ := net.Listen("tcp", "127.0.0.1:0")
	puerto := fmt.Sprint(escucha.Addr().(*net.TCPAddr).Port)
	escucha.Close()

	contexto, cancelar := context.WithCancel(context.Background())
	prueba.Cleanup(cancelar)
	go metricas.Servir(contexto, puerto)

	for intento := 0; intento < 50; intento++ {
		respuesta, err := http.Get("http://127.0.0.1:" + puerto + "/metrics")
		if err == nil {
			defer respuesta.Body.Close()
			cuerpo, _ := io.ReadAll(respuesta.Body)
			return string(cuerpo)
		}
		time.Sleep(20 * time.Millisecond)
	}
	prueba.Fatal("el servidor de métricas no arrancó")
	return ""
}

func TestLosEnviosSeCuentanPorResultado(prueba *testing.T) {
	registro := metricas.Nuevas()
	registro.ContarEnvio(puertos.EnvioExitoso)
	registro.ContarEnvio(puertos.EnvioExitoso)
	registro.ContarEnvio(puertos.EnvioReintento)

	publicado := leerMetricas(prueba, registro)

	for _, linea := range []string{
		`notificaciones_envios_total{resultado="enviada"} 2`,
		`notificaciones_envios_total{resultado="reintento"} 1`,
		`notificaciones_envios_total{resultado="fallida"} 0`, // existe desde el arranque
	} {
		if !strings.Contains(publicado, linea) {
			prueba.Errorf("falta %q en /metrics", linea)
		}
	}
	if !strings.Contains(publicado, "go_goroutines") {
		prueba.Error("faltan las métricas del runtime de Go")
	}
}

func TestElMiddlewareMideCadaSolicitudPorRuta(prueba *testing.T) {
	gin.SetMode(gin.TestMode)
	registro := metricas.Nuevas()
	router := gin.New()
	router.Use(registro.Solicitudes())
	router.GET("/notificaciones/:id", func(contexto *gin.Context) { contexto.Status(http.StatusNotFound) })

	for _, id := range []string{"uno", "dos", "tres"} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/notificaciones/"+id, nil))
	}
	publicado := leerMetricas(prueba, registro)

	if !strings.Contains(publicado, `solicitudes_http_total{estado="404",metodo="GET",ruta="/notificaciones/:id"} 3`) {
		prueba.Errorf("las 3 solicitudes tienen que contarse en la misma serie:\n%s", publicado)
	}
	if !strings.Contains(publicado, `duracion_solicitud_http_segundos_bucket{metodo="GET",ruta="/notificaciones/:id",le="0.3"} 3`) {
		prueba.Error("falta el histograma de duración con el límite de 0,3 s")
	}
	if strings.Contains(publicado, "/notificaciones/uno") {
		prueba.Error("no se tiene que crear una serie por id")
	}
}
