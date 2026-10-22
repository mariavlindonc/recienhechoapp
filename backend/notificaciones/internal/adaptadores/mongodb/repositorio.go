// Package mongodb implementa el puerto Repositorio sobre MongoDB (ADR-002).
package mongodb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

const (
	coleccionNotificaciones = "notificaciones"
	coleccionClaves         = "claves_idempotencia"

	// vigenciaClaves es cuánto se recuerda una Idempotency-Key, según el contrato.
	vigenciaClaves = 24 * time.Hour
)

// Repositorio cumple puertos.Repositorio con dos colecciones: las notificaciones y
// las claves de idempotencia. Están separadas para que la clave venza a las 24 h
// sin borrar la notificación.
type Repositorio struct {
	notificaciones *mongo.Collection
	claves         *mongo.Collection
}

var _ puertos.Repositorio = (*Repositorio)(nil)

// NuevoRepositorio usa las colecciones de la base dada.
func NuevoRepositorio(base *mongo.Database) *Repositorio {
	return &Repositorio{
		notificaciones: base.Collection(coleccionNotificaciones),
		claves:         base.Collection(coleccionClaves),
	}
}

// PrepararIndices crea los índices si no existen. Se llama una vez al arrancar.
func (repositorio *Repositorio) PrepararIndices(contexto context.Context) error {
	// Índice TTL: MongoDB borra solo cada clave cuando pasan 24 h desde creadaEn.
	_, err := repositorio.claves.Indexes().CreateOne(contexto, mongo.IndexModel{
		Keys:    bson.D{{Key: "creadaEn", Value: 1}},
		Options: options.Index().SetName("vencimiento_24h").SetExpireAfterSeconds(int32(vigenciaClaves.Seconds())),
	})
	if err != nil {
		return fmt.Errorf("crear índice de vencimiento de claves: %w", err)
	}

	// Para que el proceso de envío encuentre rápido las pendientes listas.
	_, err = repositorio.notificaciones.Indexes().CreateOne(contexto, mongo.IndexModel{
		Keys:    bson.D{{Key: "estado", Value: 1}, {Key: "proximoIntento", Value: 1}},
		Options: options.Index().SetName("pendientes_por_fecha"),
	})
	if err != nil {
		return fmt.Errorf("crear índice de pendientes: %w", err)
	}
	return nil
}

// idClave es el _id de una clave de idempotencia. Al ser el _id, MongoDB garantiza
// que no haya dos documentos con el mismo grupo y la misma clave.
type idClave struct {
	Grupo string `bson:"grupo"`
	Clave string `bson:"clave"`
}

type documentoClave struct {
	ID             idClave   `bson:"_id"`
	Huella         string    `bson:"huella"`
	IDNotificacion string    `bson:"idNotificacion"`
	CreadaEn       time.Time `bson:"creadaEn"`
}

type documentoNotificacion struct {
	ID             string     `bson:"_id"`
	Grupo          string     `bson:"grupo"`
	Canal          string     `bson:"canal"`
	Destinatario   string     `bson:"destinatario"`
	Asunto         string     `bson:"asunto"`
	Cuerpo         string     `bson:"cuerpo"`
	Estado         string     `bson:"estado"`
	Motivo         string     `bson:"motivo,omitempty"`
	Intentos       int        `bson:"intentos"`
	ProximoIntento time.Time  `bson:"proximoIntento"`
	CreadaEn       time.Time  `bson:"creadaEn"`
	EnviadaEn      *time.Time `bson:"enviadaEn,omitempty"`
}

// ReservarClave inserta la clave; si el _id ya existe, la inserción falla en forma
// atómica y se devuelve la que estaba guardada.
func (repositorio *Repositorio) ReservarClave(contexto context.Context, clave puertos.ClaveIdempotencia) (puertos.ClaveIdempotencia, bool, error) {
	documento := documentoClave{
		ID:             idClave{Grupo: clave.Grupo, Clave: clave.Clave},
		Huella:         clave.Huella,
		IDNotificacion: clave.IDNotificacion,
		CreadaEn:       clave.CreadaEn,
	}
	_, err := repositorio.claves.InsertOne(contexto, documento)
	if err == nil {
		return clave, false, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return puertos.ClaveIdempotencia{}, false, fmt.Errorf("insertar clave de idempotencia: %w", err)
	}

	var guardada documentoClave
	if err := repositorio.claves.FindOne(contexto, bson.D{{Key: "_id", Value: documento.ID}}).Decode(&guardada); err != nil {
		return puertos.ClaveIdempotencia{}, false, fmt.Errorf("leer clave de idempotencia existente: %w", err)
	}
	return puertos.ClaveIdempotencia{
		Grupo:          guardada.ID.Grupo,
		Clave:          guardada.ID.Clave,
		Huella:         guardada.Huella,
		IDNotificacion: guardada.IDNotificacion,
		CreadaEn:       guardada.CreadaEn,
	}, true, nil
}

// CrearSiNoExiste inserta la notificación; si ya hay una con ese id, no hace nada.
func (repositorio *Repositorio) CrearSiNoExiste(contexto context.Context, notificacion dominio.Notificacion) error {
	_, err := repositorio.notificaciones.InsertOne(contexto, aDocumento(notificacion))
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return fmt.Errorf("insertar notificación: %w", err)
	}
	return nil
}

// Buscar filtra por id y por grupo: la notificación de otro grupo no se encuentra.
func (repositorio *Repositorio) Buscar(contexto context.Context, grupo, id string) (dominio.Notificacion, error) {
	var documento documentoNotificacion
	err := repositorio.notificaciones.FindOne(contexto, bson.D{{Key: "_id", Value: id}, {Key: "grupo", Value: grupo}}).Decode(&documento)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return dominio.Notificacion{}, dominio.ErrNoEncontrada
	}
	if err != nil {
		return dominio.Notificacion{}, fmt.Errorf("buscar notificación: %w", err)
	}
	return aDominio(documento), nil
}

// TomarPendiente busca y aparta una pendiente en una sola operación atómica
// (findOneAndUpdate): si dos instancias la piden al mismo tiempo, solo una la obtiene.
func (repositorio *Repositorio) TomarPendiente(contexto context.Context, ahora, hasta time.Time) (dominio.Notificacion, bool, error) {
	filtro := bson.D{
		{Key: "estado", Value: string(dominio.EstadoPendiente)},
		{Key: "proximoIntento", Value: bson.D{{Key: "$lte", Value: ahora}}},
	}
	cambio := bson.D{{Key: "$set", Value: bson.D{{Key: "proximoIntento", Value: hasta}}}}
	opciones := options.FindOneAndUpdate().
		SetSort(bson.D{{Key: "proximoIntento", Value: 1}}).
		SetReturnDocument(options.After)

	var documento documentoNotificacion
	err := repositorio.notificaciones.FindOneAndUpdate(contexto, filtro, cambio, opciones).Decode(&documento)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return dominio.Notificacion{}, false, nil
	}
	if err != nil {
		return dominio.Notificacion{}, false, fmt.Errorf("tomar notificación pendiente: %w", err)
	}
	return aDominio(documento), true, nil
}

// Actualizar reemplaza el documento entero con el estado nuevo.
func (repositorio *Repositorio) Actualizar(contexto context.Context, notificacion dominio.Notificacion) error {
	resultado, err := repositorio.notificaciones.ReplaceOne(contexto, bson.D{{Key: "_id", Value: notificacion.ID}}, aDocumento(notificacion))
	if err != nil {
		return fmt.Errorf("actualizar notificación: %w", err)
	}
	if resultado.MatchedCount == 0 {
		return dominio.ErrNoEncontrada
	}
	return nil
}

func aDocumento(notificacion dominio.Notificacion) documentoNotificacion {
	documento := documentoNotificacion{
		ID:             notificacion.ID,
		Grupo:          notificacion.Grupo,
		Canal:          string(notificacion.Canal),
		Destinatario:   notificacion.Destinatario,
		Asunto:         notificacion.Asunto,
		Cuerpo:         notificacion.Cuerpo,
		Estado:         string(notificacion.Estado),
		Motivo:         notificacion.Motivo,
		Intentos:       notificacion.Intentos,
		ProximoIntento: notificacion.ProximoIntento,
		CreadaEn:       notificacion.CreadaEn,
	}
	if !notificacion.EnviadaEn.IsZero() {
		enviadaEn := notificacion.EnviadaEn
		documento.EnviadaEn = &enviadaEn
	}
	return documento
}

func aDominio(documento documentoNotificacion) dominio.Notificacion {
	notificacion := dominio.Notificacion{
		ID:             documento.ID,
		Grupo:          documento.Grupo,
		Canal:          dominio.Canal(documento.Canal),
		Destinatario:   documento.Destinatario,
		Asunto:         documento.Asunto,
		Cuerpo:         documento.Cuerpo,
		Estado:         dominio.Estado(documento.Estado),
		Motivo:         documento.Motivo,
		Intentos:       documento.Intentos,
		ProximoIntento: documento.ProximoIntento.UTC(),
		CreadaEn:       documento.CreadaEn.UTC(),
	}
	if documento.EnviadaEn != nil {
		notificacion.EnviadaEn = documento.EnviadaEn.UTC()
	}
	return notificacion
}
