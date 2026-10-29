// Package productos implementa el puerto Catalogo con una llamada HTTP al servicio
// productos (ADR-003): timeout de 1 s y 2 reintentos con espera exponencial, solo
// ante errores transitorios.
package productos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

const (
	tiempoMaximoPorIntento = time.Second
	largoMaximoRespuesta   = 64 << 10
)

// esperasEntreIntentos son las esperas antes de cada reintento (ADR-003).
var esperasEntreIntentos = []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}

// Catalogo cumple puertos.Catalogo.
type Catalogo struct {
	direccion *url.URL
	cliente   *http.Client
	esperas   []time.Duration
}

var _ puertos.Catalogo = (*Catalogo)(nil)

// NuevoCatalogo usa el servicio productos en la dirección dada (http://productos:8082).
func NuevoCatalogo(direccion *url.URL) *Catalogo {
	return &Catalogo{direccion: direccion, cliente: &http.Client{Timeout: tiempoMaximoPorIntento}, esperas: esperasEntreIntentos}
}

// respuestaProducto es lo que devuelve GET /productos/{id} de productos.
type respuestaProducto struct {
	ID               string `json:"id"`
	Nombre           string `json:"nombre"`
	PrecioCentavos   int64  `json:"precioCentavos"`
	AnticipacionDias int    `json:"anticipacionDias"`
	Disponible       bool   `json:"disponible"`
	Receta           []struct {
		Ingrediente string `json:"ingrediente"`
		Cantidad    int64  `json:"cantidad"`
		Unidad      string `json:"unidad"`
	} `json:"receta"`
}

// errorTransitorio marca las fallas que vale la pena reintentar.
type errorTransitorio struct{ causa error }

func (falla errorTransitorio) Error() string { return falla.causa.Error() }
func (falla errorTransitorio) Unwrap() error { return falla.causa }

// ObtenerProducto pide el producto. El precio que vale es este, sin caché (ADR-002).
func (catalogo *Catalogo) ObtenerProducto(contexto context.Context, id string) (dominio.Producto, error) {
	var ultimo error
	for intento := 0; intento <= len(catalogo.esperas); intento++ {
		if intento > 0 {
			select {
			case <-contexto.Done():
				return dominio.Producto{}, fmt.Errorf("%w: %w", puertos.ErrProductoNoDisponible, contexto.Err())
			case <-time.After(catalogo.esperas[intento-1]):
			}
		}
		producto, err := catalogo.pedir(contexto, id)
		var transitorio errorTransitorio
		if !errors.As(err, &transitorio) {
			return producto, err
		}
		ultimo = err
	}
	return dominio.Producto{}, fmt.Errorf("%w: %w", puertos.ErrProductoNoDisponible, ultimo)
}

func (catalogo *Catalogo) pedir(contexto context.Context, id string) (dominio.Producto, error) {
	direccion := catalogo.direccion.JoinPath("productos", id)
	solicitud, err := http.NewRequestWithContext(contexto, http.MethodGet, direccion.String(), nil)
	if err != nil {
		return dominio.Producto{}, fmt.Errorf("armar la solicitud a productos: %w", err)
	}
	// Propaga la traza: la llamada a productos queda en la misma traza del pedido.
	otel.GetTextMapPropagator().Inject(contexto, propagation.HeaderCarrier(solicitud.Header))

	respuesta, err := catalogo.cliente.Do(solicitud)
	if err != nil {
		// Timeout o sin conexión. Una respuesta que llegue tarde se descarta.
		return dominio.Producto{}, errorTransitorio{fmt.Errorf("llamar a productos: %w", err)}
	}
	defer respuesta.Body.Close()

	switch {
	case respuesta.StatusCode == http.StatusNotFound:
		return dominio.Producto{}, dominio.ErrProductoInexistente
	case respuesta.StatusCode == http.StatusBadGateway, respuesta.StatusCode == http.StatusServiceUnavailable, respuesta.StatusCode == http.StatusGatewayTimeout:
		return dominio.Producto{}, errorTransitorio{fmt.Errorf("productos respondió %d", respuesta.StatusCode)}
	case respuesta.StatusCode != http.StatusOK:
		// Un 4xx o un 500 no se arregla reintentando.
		return dominio.Producto{}, fmt.Errorf("%w: productos respondió %d", puertos.ErrProductoNoDisponible, respuesta.StatusCode)
	}

	var cuerpo respuestaProducto
	if err := json.NewDecoder(io.LimitReader(respuesta.Body, largoMaximoRespuesta)).Decode(&cuerpo); err != nil {
		return dominio.Producto{}, fmt.Errorf("%w: respuesta de productos inválida: %w", puertos.ErrProductoNoDisponible, err)
	}
	if !cuerpo.Disponible {
		return dominio.Producto{}, dominio.ErrProductoInexistente
	}
	if cuerpo.PrecioCentavos < 0 || cuerpo.AnticipacionDias < 0 {
		return dominio.Producto{}, fmt.Errorf("%w: productos devolvió un precio o una anticipación negativos", puertos.ErrProductoNoDisponible)
	}
	receta := make([]dominio.Insumo, len(cuerpo.Receta))
	for indice, insumo := range cuerpo.Receta {
		receta[indice] = dominio.Insumo{Ingrediente: insumo.Ingrediente, Cantidad: insumo.Cantidad, Unidad: insumo.Unidad}
	}
	return dominio.Producto{
		ID:               cuerpo.ID,
		Nombre:           cuerpo.Nombre,
		PrecioCentavos:   cuerpo.PrecioCentavos,
		AnticipacionDias: cuerpo.AnticipacionDias,
		Receta:           receta,
	}, nil
}
