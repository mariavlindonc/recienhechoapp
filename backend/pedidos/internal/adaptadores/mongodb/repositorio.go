// Package mongodb implementa los puertos Repositorio y BandejaDeSalida sobre
// MongoDB (ADR-002). Las operaciones que escriben en más de una colección van en
// una transacción, por eso MongoDB tiene que correr como replica set (D4).
package mongodb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

const (
	coleccionPedidos = "pedidos"
	coleccionDias    = "dias"
	coleccionEventos = "eventos_pendientes"

	// esperaPorOperacion es lo máximo que tarda cada operación simple (ADR-011).
	esperaPorOperacion = 2 * time.Second
	// esperaPorTransaccion incluye los reintentos que hace el driver cuando dos
	// transacciones tocan el mismo día al mismo tiempo.
	esperaPorTransaccion = 5 * time.Second
)

func conLimite(contexto context.Context, limite time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(contexto, limite)
}

// Repositorio cumple puertos.Repositorio y puertos.BandejaDeSalida.
type Repositorio struct {
	cliente *mongo.Client
	pedidos *mongo.Collection
	dias    *mongo.Collection
	eventos *mongo.Collection
}

var (
	_ puertos.Repositorio     = (*Repositorio)(nil)
	_ puertos.BandejaDeSalida = (*Repositorio)(nil)
)

// NuevoRepositorio usa las colecciones de la base dada.
func NuevoRepositorio(cliente *mongo.Client, base string) *Repositorio {
	baseDeDatos := cliente.Database(base)
	return &Repositorio{
		cliente: cliente,
		pedidos: baseDeDatos.Collection(coleccionPedidos),
		dias:    baseDeDatos.Collection(coleccionDias),
		eventos: baseDeDatos.Collection(coleccionEventos),
	}
}

// PrepararIndices crea los índices si no existen. Se llama una vez al arrancar.
// También crea las colecciones, que dentro de una transacción no conviene crear.
func (repositorio *Repositorio) PrepararIndices(contexto context.Context) error {
	indices := []struct {
		coleccion *mongo.Collection
		modelo    mongo.IndexModel
	}{
		// Un comprador no puede crear dos pedidos con la misma Idempotency-Key (R1).
		{repositorio.pedidos, mongo.IndexModel{
			Keys:    bson.D{{Key: "comprador.id", Value: 1}, {Key: "claveIdempotencia", Value: 1}},
			Options: options.Index().SetName("clave_por_comprador").SetUnique(true),
		}},
		{repositorio.pedidos, mongo.IndexModel{
			Keys:    bson.D{{Key: "comprador.id", Value: 1}, {Key: "creadoEn", Value: -1}},
			Options: options.Index().SetName("por_comprador"),
		}},
		{repositorio.pedidos, mongo.IndexModel{
			Keys:    bson.D{{Key: "fecha", Value: 1}},
			Options: options.Index().SetName("por_fecha"),
		}},
		{repositorio.pedidos, mongo.IndexModel{
			Keys:    bson.D{{Key: "estado", Value: 1}, {Key: "requiereRevision", Value: 1}, {Key: "proximaConfirmacion", Value: 1}},
			Options: options.Index().SetName("por_confirmar"),
		}},
		{repositorio.pedidos, mongo.IndexModel{
			Keys:    bson.D{{Key: "liberarInsumos", Value: 1}, {Key: "proximaLiberacion", Value: 1}},
			Options: options.Index().SetName("por_liberar"),
		}},
		{repositorio.eventos, mongo.IndexModel{
			Keys:    bson.D{{Key: "proximoIntento", Value: 1}, {Key: "evento.ocurridoEn", Value: 1}},
			Options: options.Index().SetName("por_publicar"),
		}},
	}
	for _, indice := range indices {
		if _, err := indice.coleccion.Indexes().CreateOne(contexto, indice.modelo); err != nil {
			return fmt.Errorf("crear índices de %s: %w", indice.coleccion.Name(), err)
		}
	}
	// dias no tiene índices propios (se busca por _id), pero la colección tiene que existir.
	if err := repositorio.dias.Database().CreateCollection(contexto, coleccionDias); err != nil && !esColeccionExistente(err) {
		return fmt.Errorf("crear colección de días: %w", err)
	}
	return nil
}

func esColeccionExistente(err error) bool {
	var errorDeComando mongo.CommandError
	return errors.As(err, &errorDeComando) && errorDeComando.Name == "NamespaceExists"
}

// enTransaccion corre la función en una transacción. El driver la reintenta sola
// si choca con otra transacción que toca los mismos documentos.
func (repositorio *Repositorio) enTransaccion(contexto context.Context, operacion func(contexto context.Context) error) error {
	contexto, cancelar := conLimite(contexto, esperaPorTransaccion)
	defer cancelar()
	sesion, err := repositorio.cliente.StartSession()
	if err != nil {
		return fmt.Errorf("abrir sesión: %w", err)
	}
	defer sesion.EndSession(contexto)
	_, err = sesion.WithTransaction(contexto, func(contextoTransaccion context.Context) (any, error) {
		return nil, operacion(contextoTransaccion)
	})
	return err
}

// Registrar guarda el pedido, reserva el cupo y guarda el evento en una sola
// transacción: si cualquiera de los tres falla, no queda ninguno (D4).
func (repositorio *Repositorio) Registrar(contexto context.Context, pedido dominio.Pedido, evento dominio.Evento) error {
	return repositorio.enTransaccion(contexto, func(contexto context.Context) error {
		if _, err := repositorio.pedidos.InsertOne(contexto, aDocumento(pedido, 0)); err != nil {
			if mongo.IsDuplicateKeyError(err) {
				return dominio.ErrPedidoRepetido
			}
			return fmt.Errorf("insertar pedido: %w", err)
		}
		if !pedido.EsperaCotizacion() {
			if err := repositorio.reservarCupo(contexto, pedido); err != nil {
				return err
			}
		}
		return repositorio.guardarEvento(contexto, evento)
	})
}

// reservarCupo es la reserva condicional: solo suma si después de sumar no se
// pasa de la capacidad. Si el día no existe, su capacidad es 0 y no hay cupo.
func (repositorio *Repositorio) reservarCupo(contexto context.Context, pedido dominio.Pedido) error {
	resultado, err := repositorio.dias.UpdateOne(contexto,
		bson.D{
			{Key: "_id", Value: pedido.Fecha},
			{Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{
				bson.D{{Key: "$add", Value: bson.A{"$reservados", pedido.Unidades()}}}, "$capacidad",
			}}}},
		},
		bson.D{{Key: "$inc", Value: bson.D{{Key: "reservados", Value: pedido.Unidades()}}}},
	)
	if err != nil {
		return fmt.Errorf("reservar cupo del %s: %w", pedido.Fecha, err)
	}
	if resultado.MatchedCount == 0 {
		return dominio.ErrSinCupo
	}
	return nil
}

// GuardarCambio reemplaza el pedido si sigue en la versión leída y, en la misma
// transacción, guarda el evento y devuelve el cupo.
func (repositorio *Repositorio) GuardarCambio(contexto context.Context, cambio puertos.Cambio) error {
	return repositorio.enTransaccion(contexto, func(contexto context.Context) error {
		resultado, err := repositorio.pedidos.ReplaceOne(contexto,
			bson.D{{Key: "_id", Value: cambio.Pedido.ID}, {Key: "version", Value: cambio.VersionAnterior}},
			aDocumento(cambio.Pedido, cambio.VersionAnterior+1),
		)
		if err != nil {
			return fmt.Errorf("actualizar pedido: %w", err)
		}
		if resultado.MatchedCount == 0 {
			return dominio.ErrModificadoAlMismoTiempo
		}
		if cambio.ReservarCupo {
			if err := repositorio.reservarCupo(contexto, cambio.Pedido); err != nil {
				return err
			}
		}
		if cambio.LiberarCupo {
			_, err := repositorio.dias.UpdateOne(contexto,
				bson.D{{Key: "_id", Value: cambio.Pedido.Fecha}},
				bson.D{{Key: "$inc", Value: bson.D{{Key: "reservados", Value: -cambio.Pedido.Unidades()}}}},
			)
			if err != nil {
				return fmt.Errorf("liberar cupo del %s: %w", cambio.Pedido.Fecha, err)
			}
		}
		if cambio.Evento != nil {
			return repositorio.guardarEvento(contexto, *cambio.Evento)
		}
		return nil
	})
}

func (repositorio *Repositorio) guardarEvento(contexto context.Context, evento dominio.Evento) error {
	documento := documentoEventoGuardado{
		ID:             evento.ID,
		Evento:         aDocumentoEvento(evento),
		Traza:          trazaDe(contexto),
		ProximoIntento: evento.OcurridoEn,
	}
	if _, err := repositorio.eventos.InsertOne(contexto, documento); err != nil {
		return fmt.Errorf("guardar evento en la bandeja de salida: %w", err)
	}
	return nil
}

// Buscar devuelve el pedido con ese id.
func (repositorio *Repositorio) Buscar(contexto context.Context, id string) (dominio.Pedido, error) {
	return repositorio.buscarUno(contexto, bson.D{{Key: "_id", Value: id}})
}

// BuscarPorClave devuelve el pedido que el comprador creó con esa clave.
func (repositorio *Repositorio) BuscarPorClave(contexto context.Context, idComprador, clave string) (dominio.Pedido, error) {
	return repositorio.buscarUno(contexto, bson.D{{Key: "comprador.id", Value: idComprador}, {Key: "claveIdempotencia", Value: clave}})
}

func (repositorio *Repositorio) buscarUno(contexto context.Context, filtro bson.D) (dominio.Pedido, error) {
	contexto, cancelar := conLimite(contexto, esperaPorOperacion)
	defer cancelar()
	var documento documentoPedido
	err := repositorio.pedidos.FindOne(contexto, filtro).Decode(&documento)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return dominio.Pedido{}, dominio.ErrNoEncontrado
	}
	if err != nil {
		return dominio.Pedido{}, fmt.Errorf("buscar pedido: %w", err)
	}
	return aDominio(documento), nil
}

// ListarDeComprador devuelve los pedidos del comprador, del más nuevo al más viejo.
func (repositorio *Repositorio) ListarDeComprador(contexto context.Context, idComprador string) ([]dominio.Pedido, error) {
	return repositorio.listar(contexto, bson.D{{Key: "comprador.id", Value: idComprador}}, bson.D{{Key: "creadoEn", Value: -1}})
}

// ListarDelDia devuelve los pedidos de esa fecha, en el orden en que se hicieron.
// Los especiales sin aceptar no se producen: no aparecen.
func (repositorio *Repositorio) ListarDelDia(contexto context.Context, fecha string) ([]dominio.Pedido, error) {
	return repositorio.listar(contexto, bson.D{{Key: "fecha", Value: fecha}, {Key: "estado", Value: bson.D{{Key: "$nin", Value: estadosSinProduccion}}}}, bson.D{{Key: "creadoEn", Value: 1}})
}

var estadosSinProduccion = bson.A{dominio.EstadoSolicitado, dominio.EstadoCotizado, dominio.EstadoRechazado, dominio.EstadoVencido}

// ListarEspecialesAbiertos devuelve los especiales que el vendedor tiene que cotizar
// o que esperan respuesta del comprador, del más viejo al más nuevo.
func (repositorio *Repositorio) ListarEspecialesAbiertos(contexto context.Context) ([]dominio.Pedido, error) {
	return repositorio.listar(contexto,
		bson.D{{Key: "tipo", Value: dominio.TipoEspecial}, {Key: "estado", Value: bson.D{{Key: "$in", Value: bson.A{dominio.EstadoSolicitado, dominio.EstadoCotizado}}}}},
		bson.D{{Key: "creadoEn", Value: 1}})
}

// shortcut: los listados no tienen paginación; alcanza para el volumen de una
// pastelería, se agrega si un comprador o un día superan unos cientos de pedidos.
func (repositorio *Repositorio) listar(contexto context.Context, filtro, orden bson.D) ([]dominio.Pedido, error) {
	contexto, cancelar := conLimite(contexto, esperaPorOperacion)
	defer cancelar()
	cursor, err := repositorio.pedidos.Find(contexto, filtro, options.Find().SetSort(orden))
	if err != nil {
		return nil, fmt.Errorf("listar pedidos: %w", err)
	}
	var documentos []documentoPedido
	if err := cursor.All(contexto, &documentos); err != nil {
		return nil, fmt.Errorf("leer pedidos: %w", err)
	}
	pedidos := make([]dominio.Pedido, len(documentos))
	for indice, documento := range documentos {
		pedidos[indice] = aDominio(documento)
	}
	return pedidos, nil
}

// TomarParaConfirmar aparta un pedido pendiente en una sola operación atómica:
// si dos instancias lo piden al mismo tiempo, solo una lo obtiene. Sube la
// versión, así un proceso que lo tenía leído de antes no lo pisa.
func (repositorio *Repositorio) TomarParaConfirmar(contexto context.Context, ahora, hasta time.Time) (dominio.Pedido, bool, error) {
	return repositorio.tomar(contexto, bson.D{
		{Key: "estado", Value: string(dominio.EstadoPendienteConfirmacion)},
		{Key: "requiereRevision", Value: false},
		{Key: "proximaConfirmacion", Value: bson.D{{Key: "$lte", Value: ahora}}},
	}, "proximaConfirmacion", hasta)
}

// TomarParaLiberar aparta un pedido con insumos por liberar.
func (repositorio *Repositorio) TomarParaLiberar(contexto context.Context, ahora, hasta time.Time) (dominio.Pedido, bool, error) {
	return repositorio.tomar(contexto, bson.D{
		{Key: "liberarInsumos", Value: true},
		{Key: "proximaLiberacion", Value: bson.D{{Key: "$lte", Value: ahora}}},
	}, "proximaLiberacion", hasta)
}

func (repositorio *Repositorio) tomar(contexto context.Context, filtro bson.D, campo string, hasta time.Time) (dominio.Pedido, bool, error) {
	contexto, cancelar := conLimite(contexto, esperaPorOperacion)
	defer cancelar()
	cambio := bson.D{
		{Key: "$set", Value: bson.D{{Key: campo, Value: hasta}}},
		{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
	}
	opciones := options.FindOneAndUpdate().SetSort(bson.D{{Key: campo, Value: 1}}).SetReturnDocument(options.After)
	var documento documentoPedido
	err := repositorio.pedidos.FindOneAndUpdate(contexto, filtro, cambio, opciones).Decode(&documento)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return dominio.Pedido{}, false, nil
	}
	if err != nil {
		return dominio.Pedido{}, false, fmt.Errorf("tomar pedido: %w", err)
	}
	return aDominio(documento), true, nil
}

// DefinirCapacidad cambia la capacidad solo si no queda por debajo de lo
// reservado, en una sola operación atómica. Si el día no existía, lo crea.
func (repositorio *Repositorio) DefinirCapacidad(contexto context.Context, fecha string, capacidad int) (dominio.Dia, error) {
	contexto, cancelar := conLimite(contexto, esperaPorOperacion)
	defer cancelar()
	opciones := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var documento documentoDia
	err := repositorio.dias.FindOneAndUpdate(contexto,
		bson.D{{Key: "_id", Value: fecha}, {Key: "reservados", Value: bson.D{{Key: "$lte", Value: capacidad}}}},
		bson.D{
			{Key: "$set", Value: bson.D{{Key: "capacidad", Value: capacidad}}},
			{Key: "$setOnInsert", Value: bson.D{{Key: "reservados", Value: 0}}},
		},
		opciones,
	).Decode(&documento)
	// Si el día existe pero tiene más reservado, el filtro no coincide y el upsert
	// intenta crear otro con el mismo _id: MongoDB lo rechaza por clave duplicada.
	if mongo.IsDuplicateKeyError(err) {
		dia, errDia := repositorio.buscarDia(contexto, fecha)
		if errDia != nil {
			return dominio.Dia{}, errDia
		}
		return dia, dominio.ErrCapacidadMenorQueReservados
	}
	if err != nil {
		return dominio.Dia{}, fmt.Errorf("definir capacidad: %w", err)
	}
	return documento.aDominio(), nil
}

func (repositorio *Repositorio) buscarDia(contexto context.Context, fecha string) (dominio.Dia, error) {
	var documento documentoDia
	if err := repositorio.dias.FindOne(contexto, bson.D{{Key: "_id", Value: fecha}}).Decode(&documento); err != nil {
		return dominio.Dia{}, fmt.Errorf("buscar día %s: %w", fecha, err)
	}
	return documento.aDominio(), nil
}

// ListarCapacidad devuelve los días definidos entre las dos fechas, inclusive.
func (repositorio *Repositorio) ListarCapacidad(contexto context.Context, desde, hasta string) ([]dominio.Dia, error) {
	contexto, cancelar := conLimite(contexto, esperaPorOperacion)
	defer cancelar()
	cursor, err := repositorio.dias.Find(contexto,
		bson.D{{Key: "_id", Value: bson.D{{Key: "$gte", Value: desde}, {Key: "$lte", Value: hasta}}}},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}),
	)
	if err != nil {
		return nil, fmt.Errorf("listar días: %w", err)
	}
	var documentos []documentoDia
	if err := cursor.All(contexto, &documentos); err != nil {
		return nil, fmt.Errorf("leer días: %w", err)
	}
	dias := make([]dominio.Dia, len(documentos))
	for indice, documento := range documentos {
		dias[indice] = documento.aDominio()
	}
	return dias, nil
}

// TomarEvento aparta el evento más viejo listo para publicarse.
func (repositorio *Repositorio) TomarEvento(contexto context.Context, ahora, hasta time.Time) (puertos.EventoGuardado, bool, error) {
	contexto, cancelar := conLimite(contexto, esperaPorOperacion)
	defer cancelar()
	opciones := options.FindOneAndUpdate().
		SetSort(bson.D{{Key: "evento.ocurridoEn", Value: 1}}).
		SetReturnDocument(options.After)
	var documento documentoEventoGuardado
	err := repositorio.eventos.FindOneAndUpdate(contexto,
		bson.D{{Key: "proximoIntento", Value: bson.D{{Key: "$lte", Value: ahora}}}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "proximoIntento", Value: hasta}}}},
		opciones,
	).Decode(&documento)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return puertos.EventoGuardado{}, false, nil
	}
	if err != nil {
		return puertos.EventoGuardado{}, false, fmt.Errorf("tomar evento: %w", err)
	}
	return puertos.EventoGuardado{Evento: documento.Evento.aDominio(), Traza: documento.Traza}, true, nil
}

// MarcarPublicado borra el evento de la bandeja: ya está en el broker.
func (repositorio *Repositorio) MarcarPublicado(contexto context.Context, idEvento string) error {
	contexto, cancelar := conLimite(contexto, esperaPorOperacion)
	defer cancelar()
	if _, err := repositorio.eventos.DeleteOne(contexto, bson.D{{Key: "_id", Value: idEvento}}); err != nil {
		return fmt.Errorf("borrar evento publicado: %w", err)
	}
	return nil
}
