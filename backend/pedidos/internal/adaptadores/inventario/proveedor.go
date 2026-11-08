package inventario

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

const (
	largoMaximoRespuesta = 64 << 10
	largoMaximoMotivo    = 200 // el contrato del proveedor (reason)
)

// Proveedor cumple puertos.ProveedorDeStock con llamadas HTTP al inventario del
// otro grupo, según la versión 2.1.0 de su contrato (ADR-020).
type Proveedor struct {
	direccion *url.URL
	clave     string
	sucursal  int64
	cliente   *http.Client
}

var _ puertos.ProveedorDeStock = (*Proveedor)(nil)

// NuevoProveedor llama a la dirección dada (https://stock-volcamelli-inventario.onrender.com)
// con la clave en X-API-Key y siempre sobre la misma sucursal. Cada llamada tiene el
// timeout de ADR-010 (TiempoMaximo).
func NuevoProveedor(direccion *url.URL, clave string, sucursal int64) *Proveedor {
	return &Proveedor{direccion: direccion, clave: clave, sucursal: sucursal, cliente: &http.Client{Timeout: TiempoMaximo}}
}

// cambioDeStock es el cuerpo de increase y decrease.
type cambioDeStock struct {
	Cantidad int64  `json:"quantity"`
	Motivo   string `json:"reason"`
}

// Reducir resta cantidad unidades del producto y devuelve el stock que queda.
func (proveedor *Proveedor) Reducir(contexto context.Context, idProducto, cantidad int64, motivo string) (int64, error) {
	return proveedor.cambiar(contexto, idProducto, "decrease", cantidad, motivo)
}

// Aumentar suma cantidad unidades del producto y devuelve el stock que queda.
func (proveedor *Proveedor) Aumentar(contexto context.Context, idProducto, cantidad int64, motivo string) (int64, error) {
	return proveedor.cambiar(contexto, idProducto, "increase", cantidad, motivo)
}

// Consultar devuelve el stock disponible del producto.
func (proveedor *Proveedor) Consultar(contexto context.Context, idProducto int64) (int64, error) {
	return proveedor.pedir(contexto, http.MethodGet, idProducto, "", nil)
}

func (proveedor *Proveedor) cambiar(contexto context.Context, idProducto int64, operacion string, cantidad int64, motivo string) (int64, error) {
	// Se valida acá lo que el proveedor rechazaría con 400: no vale la pena llamarlo.
	if cantidad <= 0 || motivo == "" || utf8.RuneCountInString(motivo) > largoMaximoMotivo {
		return 0, fmt.Errorf("%w: cantidad %d o motivo inválidos", puertos.ErrInventarioRechazo, cantidad)
	}
	cuerpo, err := json.Marshal(cambioDeStock{Cantidad: cantidad, Motivo: motivo})
	if err != nil {
		return 0, fmt.Errorf("armar el cuerpo para el inventario: %w", err)
	}
	return proveedor.pedir(contexto, http.MethodPost, idProducto, operacion, cuerpo)
}

func (proveedor *Proveedor) pedir(contexto context.Context, metodo string, idProducto int64, operacion string, cuerpo []byte) (int64, error) {
	partes := []string{"api", "branches", strconv.FormatInt(proveedor.sucursal, 10), "products", strconv.FormatInt(idProducto, 10), "stock"}
	if operacion != "" {
		partes = append(partes, operacion)
	}
	solicitud, err := http.NewRequestWithContext(contexto, metodo, proveedor.direccion.JoinPath(partes...).String(), bytes.NewReader(cuerpo))
	if err != nil {
		return 0, fmt.Errorf("armar la solicitud al inventario: %w", err)
	}
	solicitud.Header.Set("X-API-Key", proveedor.clave)
	if cuerpo != nil {
		solicitud.Header.Set("Content-Type", "application/json")
	}

	respuesta, err := proveedor.cliente.Do(solicitud)
	if err != nil {
		// Timeout o sin conexión: no se sabe si el cambio se aplicó.
		return 0, fmt.Errorf("llamar al inventario: %w", err)
	}
	defer respuesta.Body.Close()

	switch {
	case respuesta.StatusCode == http.StatusConflict:
		return 0, fmt.Errorf("%w: respondió 409", puertos.ErrStockInsuficiente)
	case respuesta.StatusCode >= 400 && respuesta.StatusCode < 500:
		return 0, fmt.Errorf("%w: respondió %d", puertos.ErrInventarioRechazo, respuesta.StatusCode)
	case respuesta.StatusCode != http.StatusOK:
		return 0, fmt.Errorf("el inventario respondió %d", respuesta.StatusCode)
	}

	var stock struct {
		Cantidad *int64 `json:"quantity"`
	}
	if err := json.NewDecoder(io.LimitReader(respuesta.Body, largoMaximoRespuesta)).Decode(&stock); err != nil || stock.Cantidad == nil {
		return 0, fmt.Errorf("respuesta del inventario inválida: %v", err)
	}
	return *stock.Cantidad, nil
}
