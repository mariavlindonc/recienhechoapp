package dominio

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Pedidos especiales (C6, VE5, R8): pasan por solicitado → cotizado y de ahí a
// rechazado, vencido o, al aceptarlos, a pendiente_confirmacion, donde siguen como
// cualquier pedido: reservan el cupo del día y los confirma el inventario
// (docs/ARCHITECTURE.md, "Estados de un pedido"; ADR-017).
const (
	TipoEspecial Tipo = "especial"

	EstadoSolicitado Estado = "solicitado"
	EstadoCotizado   Estado = "cotizado"
	EstadoRechazado  Estado = "rechazado"
	EstadoVencido    Estado = "vencido"

	// VigenciaCotizacion es cuánto tiene el comprador para aceptar una cotización (R8).
	VigenciaCotizacion = 48 * time.Hour

	largoMaximoDescripcion = 500
)

// NuevoEspecial arma la solicitud de un pedido a medida para una fecha. Lleva un
// solo ítem con la descripción, que ocupa una unidad del cupo del día cuando se
// acepta; el precio lo pone el vendedor al cotizar.
func NuevoEspecial(id string, comprador Comprador, clave, descripcion, fecha string, ahora time.Time) (Pedido, error) {
	if id == "" || comprador.ID == "" {
		return Pedido{}, fmt.Errorf("crear pedido especial: faltan el id o el comprador")
	}
	descripcion = strings.TrimSpace(descripcion)
	var problemas []string
	if descripcion == "" || utf8.RuneCountInString(descripcion) > largoMaximoDescripcion {
		problemas = append(problemas, fmt.Sprintf("descripcion tiene que tener entre 1 y %d caracteres", largoMaximoDescripcion))
	}
	if ValidarFecha(fecha) != nil {
		problemas = append(problemas, "fecha tiene que ser AAAA-MM-DD")
	} else if fecha < Hoy(ahora) {
		problemas = append(problemas, "la fecha no puede ser anterior a hoy")
	}
	if len(problemas) > 0 {
		return Pedido{}, fmt.Errorf("%w: %s", ErrSolicitudInvalida, strings.Join(problemas, "; "))
	}
	return Pedido{
		ID:                id,
		Comprador:         comprador,
		Items:             []Item{{Nombre: descripcion, Cantidad: 1}},
		Fecha:             fecha,
		Tipo:              TipoEspecial,
		Estado:            EstadoSolicitado,
		ClaveIdempotencia: clave,
		Huella:            HuellaEspecial(descripcion, fecha),
		CreadoEn:          ahora,
		ActualizadoEn:     ahora,
	}, nil
}

// HuellaEspecial identifica el contenido de la solicitud, para la idempotencia.
func HuellaEspecial(descripcion, fecha string) string {
	suma := sha256.Sum256([]byte(string(TipoEspecial) + "\x00" + strings.TrimSpace(descripcion) + "\x00" + fecha))
	return hex.EncodeToString(suma[:])
}

// EsperaCotizacion dice si es un especial todavía sin aceptar: no ocupa cupo hasta
// que el comprador acepte la cotización.
func (pedido Pedido) EsperaCotizacion() bool {
	return pedido.Estado == EstadoSolicitado || pedido.Estado == EstadoCotizado
}

// Cotizar fija el precio de un especial solicitado (VE5) y devuelve el estado anterior.
func (pedido *Pedido) Cotizar(precioCentavos int64, ahora time.Time) (Estado, error) {
	anterior := pedido.Estado
	if pedido.Tipo != TipoEspecial || anterior != EstadoSolicitado {
		return anterior, fmt.Errorf("%w: solo se cotiza un pedido especial solicitado", ErrTransicionInvalida)
	}
	if precioCentavos <= 0 {
		return anterior, fmt.Errorf("%w: precioCentavos tiene que ser mayor a cero", ErrSolicitudInvalida)
	}
	pedido.Items[0].PrecioUnitarioCentavos = precioCentavos
	pedido.Items[0].SubtotalCentavos = precioCentavos
	pedido.TotalCentavos = precioCentavos
	pedido.CotizadoEn = ahora
	pedido.Estado = EstadoCotizado
	pedido.ActualizadoEn = ahora
	return anterior, nil
}

// ResponderCotizacion acepta o rechaza la cotización (C6). Aceptar la deja
// pendiente_confirmacion para que reserve el cupo y la confirme el inventario.
// Pasadas 48 h el especial queda vencido y devuelve ErrCotizacionVencida (R8): el
// cambio igual hay que guardarlo.
func (pedido *Pedido) ResponderCotizacion(acepta bool, ahora time.Time) (Estado, error) {
	anterior := pedido.Estado
	if pedido.Tipo != TipoEspecial || anterior != EstadoCotizado {
		return anterior, fmt.Errorf("%w: solo se responde un pedido especial cotizado", ErrTransicionInvalida)
	}
	pedido.ActualizadoEn = ahora
	switch {
	case ahora.Sub(pedido.CotizadoEn) > VigenciaCotizacion:
		pedido.Estado = EstadoVencido
		return anterior, ErrCotizacionVencida
	case acepta:
		pedido.Estado = EstadoPendienteConfirmacion
		pedido.ProximaConfirmacion = ahora
	default:
		pedido.Estado = EstadoRechazado
	}
	return anterior, nil
}
