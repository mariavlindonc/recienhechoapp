package mongodb

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

// Existencias cumple puertos.Existencias sobre la base de inventario (ADR-020).
type Existencias struct {
	existencias *mongo.Collection
	// productos son los ids del proveedor que se pueden usar de casillero.
	productos []int64
}

var _ puertos.Existencias = (*Existencias)(nil)

// NuevasExistencias usa la base de inventario. productos son los productos ya
// cargados en el proveedor que sirven de casillero, en el orden en que se reparten.
func NuevasExistencias(cliente *mongo.Client, baseInventario string, productos []int64) *Existencias {
	return &Existencias{existencias: cliente.Database(baseInventario).Collection(coleccionExistencias), productos: productos}
}

// Listar devuelve las existencias conocidas, ordenadas por ingrediente.
func (existencias *Existencias) Listar(contexto context.Context) ([]dominio.Existencia, error) {
	contexto, cancelar := conLimite(contexto, esperaPorOperacion)
	defer cancelar()
	cursor, err := existencias.existencias.Find(contexto, bson.D{}, options.Find().SetSort(bson.D{{Key: "ingrediente", Value: 1}, {Key: "unidad", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("listar existencias: %w", err)
	}
	var documentos []documentoExistencia
	if err := cursor.All(contexto, &documentos); err != nil {
		return nil, fmt.Errorf("leer existencias: %w", err)
	}
	resultado := make([]dominio.Existencia, len(documentos))
	for indice, documento := range documentos {
		resultado[indice] = documento.aDominio()
	}
	return resultado, nil
}

// Obtener devuelve la existencia del ingrediente y la crea en cero si no estaba.
func (existencias *Existencias) Obtener(contexto context.Context, ingrediente, unidad string) (dominio.Existencia, error) {
	contexto, cancelar := conLimite(contexto, esperaPorOperacion)
	defer cancelar()
	var documento documentoExistencia
	err := existencias.existencias.FindOneAndUpdate(contexto,
		bson.D{{Key: "_id", Value: idExistencia(ingrediente, unidad)}},
		bson.D{{Key: "$setOnInsert", Value: bson.D{
			{Key: "ingrediente", Value: ingrediente}, {Key: "unidad", Value: unidad},
			{Key: "stock", Value: int64(0)}, {Key: "reservado", Value: int64(0)}, {Key: "productoId", Value: int64(0)},
		}}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&documento)
	if err != nil {
		return dominio.Existencia{}, fmt.Errorf("obtener la existencia de %s: %w", ingrediente, err)
	}
	return documento.aDominio(), nil
}

// AsignarProducto le da al ingrediente el primer producto del proveedor que nadie
// usa. El índice único del repositorio evita que dos ingredientes se queden con el
// mismo cuando piden a la vez: el que pierde vuelve a mirar cuáles quedan.
func (existencias *Existencias) AsignarProducto(contexto context.Context, ingrediente, unidad string) (dominio.Existencia, error) {
	actual, err := existencias.Obtener(contexto, ingrediente, unidad)
	if err != nil || actual.ProductoID != 0 {
		return actual, err
	}
	for range len(existencias.productos) + 1 {
		libre, hay, err := existencias.primerProductoLibre(contexto)
		if err != nil {
			return dominio.Existencia{}, err
		}
		if !hay {
			return dominio.Existencia{}, fmt.Errorf("%w: %s", puertos.ErrSinProductoDelProveedor, ingrediente)
		}
		contextoAsignar, cancelar := conLimite(contexto, esperaPorOperacion)
		_, err = existencias.existencias.UpdateOne(contextoAsignar,
			bson.D{{Key: "_id", Value: idExistencia(ingrediente, unidad)}, {Key: "productoId", Value: int64(0)}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "productoId", Value: libre}}}},
		)
		cancelar()
		if mongo.IsDuplicateKeyError(err) {
			continue
		}
		if err != nil {
			return dominio.Existencia{}, fmt.Errorf("asignar producto a %s: %w", ingrediente, err)
		}
		return existencias.Obtener(contexto, ingrediente, unidad)
	}
	return dominio.Existencia{}, fmt.Errorf("asignar producto a %s: demasiados choques con otras asignaciones", ingrediente)
}

func (existencias *Existencias) primerProductoLibre(contexto context.Context) (int64, bool, error) {
	contexto, cancelar := conLimite(contexto, esperaPorOperacion)
	defer cancelar()
	var usados []int64
	cursor, err := existencias.existencias.Find(contexto, bson.D{{Key: "productoId", Value: bson.D{{Key: "$gt", Value: 0}}}})
	if err != nil {
		return 0, false, fmt.Errorf("buscar productos usados: %w", err)
	}
	var documentos []documentoExistencia
	if err := cursor.All(contexto, &documentos); err != nil {
		return 0, false, fmt.Errorf("leer productos usados: %w", err)
	}
	for _, documento := range documentos {
		usados = append(usados, documento.ProductoID)
	}
	for _, producto := range existencias.productos {
		if !slices.Contains(usados, producto) {
			return producto, true, nil
		}
	}
	return 0, false, nil
}

// GuardarStock deja como stock conocido el que informó el proveedor.
func (existencias *Existencias) GuardarStock(contexto context.Context, ingrediente, unidad string, stock int64) error {
	contexto, cancelar := conLimite(contexto, esperaPorOperacion)
	defer cancelar()
	resultado, err := existencias.existencias.UpdateOne(contexto,
		bson.D{{Key: "_id", Value: idExistencia(ingrediente, unidad)}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "stock", Value: stock}}}},
	)
	if err != nil {
		return fmt.Errorf("guardar el stock de %s: %w", ingrediente, err)
	}
	if resultado.MatchedCount == 0 {
		return errors.New("guardar el stock de " + ingrediente + ": la existencia no existe")
	}
	return nil
}
