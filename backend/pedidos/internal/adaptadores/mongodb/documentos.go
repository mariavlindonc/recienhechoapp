package mongodb

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
)

// El esquema de los documentos vive acá: MongoDB no lo controla (ADR-002).

type documentoComprador struct {
	ID    string `bson:"id"`
	Email string `bson:"email"`
}

type documentoInsumo struct {
	Ingrediente string `bson:"ingrediente"`
	Cantidad    int64  `bson:"cantidad"`
	Unidad      string `bson:"unidad"`
}

type documentoPedido struct {
	ID                     string             `bson:"_id"`
	Comprador              documentoComprador `bson:"comprador"`
	ProductoID             string             `bson:"productoId"`
	NombreProducto         string             `bson:"nombreProducto"`
	Cantidad               int                `bson:"cantidad"`
	PrecioUnitarioCentavos int64              `bson:"precioUnitarioCentavos"`
	TotalCentavos          int64              `bson:"totalCentavos"`
	Insumos                []documentoInsumo  `bson:"insumos"`
	Fecha                  string             `bson:"fecha"`
	Franja                 string             `bson:"franja,omitempty"`
	Tipo                   string             `bson:"tipo"`
	Estado                 string             `bson:"estado"`
	MotivoCancelacion      string             `bson:"motivoCancelacion,omitempty"`
	ClaveIdempotencia      string             `bson:"claveIdempotencia"`
	Huella                 string             `bson:"huella"`
	IntentosConfirmacion   int                `bson:"intentosConfirmacion"`
	ProximaConfirmacion    time.Time          `bson:"proximaConfirmacion"`
	RequiereRevision       bool               `bson:"requiereRevision"`
	MotivoRevision         string             `bson:"motivoRevision,omitempty"`
	LiberarInsumos         bool               `bson:"liberarInsumos"`
	ProximaLiberacion      time.Time          `bson:"proximaLiberacion"`
	CreadoEn               time.Time          `bson:"creadoEn"`
	ActualizadoEn          time.Time          `bson:"actualizadoEn"`
	Version                int                `bson:"version"`
}

type documentoDia struct {
	Fecha      string `bson:"_id"`
	Capacidad  int    `bson:"capacidad"`
	Reservados int    `bson:"reservados"`
}

func (documento documentoDia) aDominio() dominio.Dia {
	return dominio.Dia{Fecha: documento.Fecha, Capacidad: documento.Capacidad, Reservados: documento.Reservados}
}

type documentoEvento struct {
	ID         string                      `bson:"idEvento"`
	Tipo       string                      `bson:"tipo"`
	Version    int                         `bson:"version"`
	OcurridoEn time.Time                   `bson:"ocurridoEn"`
	Datos      dominio.DatosEstadoCambiado `bson:"datos"`
}

func (documento documentoEvento) aDominio() dominio.Evento {
	return dominio.Evento{
		ID:         documento.ID,
		Tipo:       documento.Tipo,
		Version:    documento.Version,
		OcurridoEn: documento.OcurridoEn.UTC(),
		Datos:      documento.Datos,
	}
}

func aDocumentoEvento(evento dominio.Evento) documentoEvento {
	return documentoEvento{ID: evento.ID, Tipo: evento.Tipo, Version: evento.Version, OcurridoEn: evento.OcurridoEn, Datos: evento.Datos}
}

// documentoEventoGuardado es un evento en la bandeja de salida.
type documentoEventoGuardado struct {
	ID             string            `bson:"_id"`
	Evento         documentoEvento   `bson:"evento"`
	Traza          map[string]string `bson:"traza,omitempty"`
	ProximoIntento time.Time         `bson:"proximoIntento"`
}

// trazaDe guarda el traceparent de la solicitud que generó el evento: al
// publicarlo, la traza sigue en el broker y en notificaciones (ADR-006).
func trazaDe(contexto context.Context) map[string]string {
	portador := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(contexto, portador)
	if len(portador) == 0 {
		return nil
	}
	return portador
}

func aDocumento(pedido dominio.Pedido, version int) documentoPedido {
	insumos := make([]documentoInsumo, len(pedido.Insumos))
	for indice, insumo := range pedido.Insumos {
		insumos[indice] = documentoInsumo(insumo)
	}
	return documentoPedido{
		ID:                     pedido.ID,
		Comprador:              documentoComprador(pedido.Comprador),
		ProductoID:             pedido.ProductoID,
		NombreProducto:         pedido.NombreProducto,
		Cantidad:               pedido.Cantidad,
		PrecioUnitarioCentavos: pedido.PrecioUnitarioCentavos,
		TotalCentavos:          pedido.TotalCentavos,
		Insumos:                insumos,
		Fecha:                  pedido.Fecha,
		Franja:                 pedido.Franja,
		Tipo:                   string(pedido.Tipo),
		Estado:                 string(pedido.Estado),
		MotivoCancelacion:      pedido.MotivoCancelacion,
		ClaveIdempotencia:      pedido.ClaveIdempotencia,
		Huella:                 pedido.Huella,
		IntentosConfirmacion:   pedido.IntentosConfirmacion,
		ProximaConfirmacion:    pedido.ProximaConfirmacion,
		RequiereRevision:       pedido.RequiereRevision,
		MotivoRevision:         pedido.MotivoRevision,
		LiberarInsumos:         pedido.LiberarInsumos,
		ProximaLiberacion:      pedido.ProximaLiberacion,
		CreadoEn:               pedido.CreadoEn,
		ActualizadoEn:          pedido.ActualizadoEn,
		Version:                version,
	}
}

func aDominio(documento documentoPedido) dominio.Pedido {
	insumos := make([]dominio.Insumo, len(documento.Insumos))
	for indice, insumo := range documento.Insumos {
		insumos[indice] = dominio.Insumo(insumo)
	}
	return dominio.Pedido{
		ID:                     documento.ID,
		Comprador:              dominio.Comprador(documento.Comprador),
		ProductoID:             documento.ProductoID,
		NombreProducto:         documento.NombreProducto,
		Cantidad:               documento.Cantidad,
		PrecioUnitarioCentavos: documento.PrecioUnitarioCentavos,
		TotalCentavos:          documento.TotalCentavos,
		Insumos:                insumos,
		Fecha:                  documento.Fecha,
		Franja:                 documento.Franja,
		Tipo:                   dominio.Tipo(documento.Tipo),
		Estado:                 dominio.Estado(documento.Estado),
		MotivoCancelacion:      documento.MotivoCancelacion,
		ClaveIdempotencia:      documento.ClaveIdempotencia,
		Huella:                 documento.Huella,
		IntentosConfirmacion:   documento.IntentosConfirmacion,
		ProximaConfirmacion:    documento.ProximaConfirmacion.UTC(),
		RequiereRevision:       documento.RequiereRevision,
		MotivoRevision:         documento.MotivoRevision,
		LiberarInsumos:         documento.LiberarInsumos,
		ProximaLiberacion:      documento.ProximaLiberacion.UTC(),
		CreadoEn:               documento.CreadoEn.UTC(),
		ActualizadoEn:          documento.ActualizadoEn.UTC(),
		Version:                documento.Version,
	}
}
