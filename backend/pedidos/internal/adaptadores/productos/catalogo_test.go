package productos

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

// torta tiene la forma de GET /internos/productos/{id} de productos.
const torta = `{"id":"torta","nombre":"Torta","precioCentavos":1250000,"anticipacionMinimaDias":3,"disponible":true,
	"receta":[{"ingrediente":"harina","cantidad":500,"unidad":"g"}]}`

// servidor responde con los estados dados, uno por llamada, y después con el último.
func servidor(prueba *testing.T, estados ...int) (*Catalogo, *atomic.Int32) {
	prueba.Helper()
	var llamadas atomic.Int32
	productos := httptest.NewServer(http.HandlerFunc(func(escritor http.ResponseWriter, pedido *http.Request) {
		numero := int(llamadas.Add(1)) - 1
		estado := estados[min(numero, len(estados)-1)]
		if pedido.URL.Path != "/internos/productos/torta" {
			estado = http.StatusNotFound
		}
		escritor.WriteHeader(estado)
		if estado == http.StatusOK {
			escritor.Write([]byte(torta))
		}
	}))
	prueba.Cleanup(productos.Close)
	direccion, _ := url.Parse(productos.URL)
	catalogo := NuevoCatalogo(direccion)
	catalogo.esperas = []time.Duration{time.Millisecond, time.Millisecond}
	return catalogo, &llamadas
}

func TestObtenerProducto(prueba *testing.T) {
	catalogo, _ := servidor(prueba, http.StatusOK)

	producto, err := catalogo.ObtenerProducto(context.Background(), "torta")

	if err != nil || producto.PrecioCentavos != 1250000 || producto.AnticipacionDias != 3 || producto.Receta[0].Cantidad != 500 {
		prueba.Fatalf("producto %+v (%v)", producto, err)
	}
}

func TestReintentaErroresTransitorios(prueba *testing.T) {
	catalogo, llamadas := servidor(prueba, http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusOK)

	if _, err := catalogo.ObtenerProducto(context.Background(), "torta"); err != nil {
		prueba.Fatalf("al tercer intento tenía que salir: %v", err)
	}
	if llamadas.Load() != 3 {
		prueba.Errorf("llamadas = %d, se esperaban 3", llamadas.Load())
	}
}

func TestDespuesDeDosReintentosNoEstaDisponible(prueba *testing.T) {
	catalogo, llamadas := servidor(prueba, http.StatusServiceUnavailable)

	_, err := catalogo.ObtenerProducto(context.Background(), "torta")

	if !errors.Is(err, puertos.ErrProductoNoDisponible) || llamadas.Load() != 3 {
		prueba.Errorf("se esperaba no disponible tras 3 llamadas: %v (%d)", err, llamadas.Load())
	}
}

func TestNoReintentaLoQueNoSeArreglaReintentando(prueba *testing.T) {
	catalogo, llamadas := servidor(prueba, http.StatusOK)
	if _, err := catalogo.ObtenerProducto(context.Background(), "alfajor"); !errors.Is(err, dominio.ErrProductoInexistente) || llamadas.Load() != 1 {
		prueba.Errorf("un 404 es producto inexistente, sin reintentos: %v (%d)", err, llamadas.Load())
	}

	catalogo, llamadas = servidor(prueba, http.StatusInternalServerError)
	if _, err := catalogo.ObtenerProducto(context.Background(), "torta"); !errors.Is(err, puertos.ErrProductoNoDisponible) || llamadas.Load() != 1 {
		prueba.Errorf("un 500 no se reintenta: %v (%d)", err, llamadas.Load())
	}
}

func TestCortaAlSegundo(prueba *testing.T) {
	lento := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(2 * tiempoMaximoPorIntento)
	}))
	prueba.Cleanup(lento.Close)
	direccion, _ := url.Parse(lento.URL)
	catalogo := NuevoCatalogo(direccion)
	catalogo.esperas = nil

	inicio := time.Now()
	_, err := catalogo.ObtenerProducto(context.Background(), "torta")

	if !errors.Is(err, puertos.ErrProductoNoDisponible) {
		prueba.Errorf("se esperaba no disponible, dio: %v", err)
	}
	if demora := time.Since(inicio); demora > tiempoMaximoPorIntento+500*time.Millisecond {
		prueba.Errorf("tardó %v, el timeout es de %v", demora, tiempoMaximoPorIntento)
	}
}
