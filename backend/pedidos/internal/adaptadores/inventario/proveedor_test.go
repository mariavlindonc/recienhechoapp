package inventario_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/adaptadores/inventario"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

func proveedorContra(prueba *testing.T, manejador http.HandlerFunc) *inventario.Proveedor {
	prueba.Helper()
	servidor := httptest.NewServer(manejador)
	prueba.Cleanup(servidor.Close)
	direccion, err := url.Parse(servidor.URL)
	if err != nil {
		prueba.Fatal(err)
	}
	return inventario.NuevoProveedor(direccion, "clave-de-prueba", 1)
}

func TestReducirArmaLaSolicitudDelContrato(prueba *testing.T) {
	proveedor := proveedorContra(prueba, func(respuesta http.ResponseWriter, solicitud *http.Request) {
		if solicitud.Method != http.MethodPost || solicitud.URL.Path != "/api/branches/1/products/7/stock/decrease" {
			prueba.Errorf("solicitud inesperada: %s %s", solicitud.Method, solicitud.URL.Path)
		}
		if solicitud.Header.Get("X-API-Key") != "clave-de-prueba" {
			prueba.Errorf("X-API-Key = %q", solicitud.Header.Get("X-API-Key"))
		}
		var cuerpo map[string]any
		if err := json.NewDecoder(solicitud.Body).Decode(&cuerpo); err != nil || cuerpo["quantity"] != float64(500) || cuerpo["reason"] != "pedido 123" {
			prueba.Errorf("cuerpo = %v (%v)", cuerpo, err)
		}
		respuesta.Write([]byte(`{"branchId":1,"productId":7,"productName":"Harina","quantity":1500}`))
	})

	stock, err := proveedor.Reducir(context.Background(), 7, 500, "pedido 123")

	if err != nil || stock != 1500 {
		prueba.Fatalf("stock = %d, err = %v", stock, err)
	}
}

func TestAumentarYConsultar(prueba *testing.T) {
	proveedor := proveedorContra(prueba, func(respuesta http.ResponseWriter, solicitud *http.Request) {
		switch solicitud.URL.Path {
		case "/api/branches/1/products/7/stock/increase":
			respuesta.Write([]byte(`{"quantity":2000}`))
		case "/api/branches/1/products/7/stock":
			respuesta.Write([]byte(`{"quantity":1000}`))
		default:
			prueba.Errorf("ruta inesperada: %s", solicitud.URL.Path)
		}
	})

	aumentado, errAumentar := proveedor.Aumentar(context.Background(), 7, 1000, "carga")
	consultado, errConsultar := proveedor.Consultar(context.Background(), 7)

	if errAumentar != nil || aumentado != 2000 || errConsultar != nil || consultado != 1000 {
		prueba.Fatalf("aumentado = %d (%v), consultado = %d (%v)", aumentado, errAumentar, consultado, errConsultar)
	}
}

func TestTraduceLosErroresDelProveedor(prueba *testing.T) {
	casos := []struct {
		nombre         string
		estado         int
		esperado       error // nil: error transitorio
		transitorioSin []error
	}{
		{"409 es stock insuficiente", http.StatusConflict, puertos.ErrStockInsuficiente, nil},
		{"400 es rechazo", http.StatusBadRequest, puertos.ErrInventarioRechazo, nil},
		{"401 es rechazo", http.StatusUnauthorized, puertos.ErrInventarioRechazo, nil},
		{"404 es rechazo", http.StatusNotFound, puertos.ErrInventarioRechazo, nil},
		{"500 es transitorio", http.StatusInternalServerError, nil, []error{puertos.ErrStockInsuficiente, puertos.ErrInventarioRechazo}},
		{"503 de Render es transitorio", http.StatusServiceUnavailable, nil, []error{puertos.ErrStockInsuficiente, puertos.ErrInventarioRechazo}},
	}
	for _, caso := range casos {
		prueba.Run(caso.nombre, func(prueba *testing.T) {
			proveedor := proveedorContra(prueba, func(respuesta http.ResponseWriter, _ *http.Request) {
				respuesta.WriteHeader(caso.estado)
			})

			_, err := proveedor.Reducir(context.Background(), 7, 2, "pedido")

			if err == nil {
				prueba.Fatal("esperaba un error")
			}
			if caso.esperado != nil && !errors.Is(err, caso.esperado) {
				prueba.Errorf("err = %v, esperaba %v", err, caso.esperado)
			}
			for _, noEsperado := range caso.transitorioSin {
				if errors.Is(err, noEsperado) {
					prueba.Errorf("err = %v no tendría que ser %v", err, noEsperado)
				}
			}
		})
	}
}

func TestSinConexionEsTransitorio(prueba *testing.T) {
	servidor := httptest.NewServer(http.NotFoundHandler())
	direccion, _ := url.Parse(servidor.URL)
	servidor.Close()
	proveedor := inventario.NuevoProveedor(direccion, "clave", 1)

	_, err := proveedor.Consultar(context.Background(), 7)

	if err == nil || errors.Is(err, puertos.ErrInventarioRechazo) || errors.Is(err, puertos.ErrStockInsuficiente) {
		prueba.Fatalf("err = %v, esperaba un error transitorio", err)
	}
}

func TestNoLlamaConDatosQueElProveedorRechazaria(prueba *testing.T) {
	llamadas := 0
	proveedor := proveedorContra(prueba, func(http.ResponseWriter, *http.Request) { llamadas++ })

	for nombre, cambio := range map[string]func() error{
		"cantidad cero": func() error { _, err := proveedor.Reducir(context.Background(), 7, 0, "x"); return err },
		"sin motivo":    func() error { _, err := proveedor.Aumentar(context.Background(), 7, 1, ""); return err },
		"motivo largo": func() error {
			_, err := proveedor.Reducir(context.Background(), 7, 1, strings.Repeat("a", 201))
			return err
		},
		"cantidad negativa": func() error { _, err := proveedor.Aumentar(context.Background(), 7, -3, "x"); return err },
	} {
		if err := cambio(); !errors.Is(err, puertos.ErrInventarioRechazo) {
			prueba.Errorf("%s: err = %v", nombre, err)
		}
	}
	if llamadas != 0 {
		prueba.Errorf("llamó al proveedor %d veces", llamadas)
	}
}

func TestRespuestaSinCantidadEsInvalida(prueba *testing.T) {
	proveedor := proveedorContra(prueba, func(respuesta http.ResponseWriter, _ *http.Request) {
		respuesta.Write([]byte(`{"productId":7}`))
	})

	if _, err := proveedor.Consultar(context.Background(), 7); err == nil {
		prueba.Fatal("esperaba un error por la respuesta sin quantity")
	}
}

// TestContratoConElMock corre el adaptador real contra Prism levantado con la copia
// del contrato del proveedor (ADR-020). Si actualizar la copia rompe el formato,
// falla acá. Prism responde los ejemplos del contrato, sin estado.
// Se corre con: docker compose up -d inventario-mock y
// INVENTARIO_MOCK_URL_TEST=http://localhost:4011.
func TestContratoConElMock(prueba *testing.T) {
	texto := os.Getenv("INVENTARIO_MOCK_URL_TEST")
	if texto == "" {
		prueba.Skip("INVENTARIO_MOCK_URL_TEST no está definida: se saltea el test de contrato")
	}
	direccion, err := url.Parse(texto)
	if err != nil {
		prueba.Fatal(err)
	}
	proveedor := inventario.NuevoProveedor(direccion, "cualquiera", 1)
	contexto := context.Background()

	if stock, err := proveedor.Consultar(contexto, 15); err != nil || stock != 42 {
		prueba.Errorf("Consultar = %d, %v; el ejemplo del contrato es 42", stock, err)
	}
	if stock, err := proveedor.Reducir(contexto, 15, 2, "Venta"); err != nil || stock != 40 {
		prueba.Errorf("Reducir = %d, %v; el ejemplo del contrato es 40", stock, err)
	}
	if stock, err := proveedor.Aumentar(contexto, 15, 5, "Llegó mercadería"); err != nil || stock != 47 {
		prueba.Errorf("Aumentar = %d, %v; el ejemplo del contrato es 47", stock, err)
	}
	// Sin clave el mock responde 401, que es un rechazo.
	sinClave := inventario.NuevoProveedor(direccion, "", 1)
	if _, err := sinClave.Consultar(contexto, 15); !errors.Is(err, puertos.ErrInventarioRechazo) {
		prueba.Errorf("sin clave: err = %v, esperaba rechazo", err)
	}
}
