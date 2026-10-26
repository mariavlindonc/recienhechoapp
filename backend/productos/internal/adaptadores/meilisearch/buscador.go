// Package meilisearch implementa el puerto Buscador con Meilisearch (ADR-008). Si
// la cátedra pidiera otro motor, se cambia este paquete y nada más.
package meilisearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	meili "github.com/meilisearch/meilisearch-go"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
)

const (
	indiceVigente        = "productos"
	indiceReconstruccion = "productos_reconstruccion"
	clavePrimaria        = "id"

	tiempoDeBusqueda      = 2 * time.Second
	tiempoDeIndexado      = 30 * time.Second // agregar o borrar y esperar a que Meilisearch termine
	esperaEntreRevisiones = 50 * time.Millisecond
)

// configuracion del índice (ADR-008). El orden de los buscables es el de relevancia.
var configuracion = &meili.Settings{
	SearchableAttributes: []string{"nombre", "descripcion", "categoria"},
	FilterableAttributes: []string{"categoria", "precioCentavos", "aptoCeliacos", "disponible"},
	SortableAttributes:   []string{"precioCentavos", "creadoEn"},
}

// documento es un producto tal como se guarda en el índice. Sin receta.
type documento struct {
	ID                     string `json:"id"`
	Nombre                 string `json:"nombre"`
	Descripcion            string `json:"descripcion"`
	CategoriaID            string `json:"categoriaId"`
	Categoria              string `json:"categoria"`
	PrecioCentavos         int64  `json:"precioCentavos"`
	AnticipacionMinimaDias int    `json:"anticipacionMinimaDias"`
	AptoCeliacos           bool   `json:"aptoCeliacos"`
	Disponible             bool   `json:"disponible"`
	ImagenURL              string `json:"imagenUrl"`
	CreadoEn               int64  `json:"creadoEn"` // milisegundos desde 1970, para ordenar por novedad
}

func documentoDe(producto dominio.Producto) documento {
	return documento{
		ID:                     producto.ID,
		Nombre:                 producto.Nombre,
		Descripcion:            producto.Descripcion,
		CategoriaID:            producto.CategoriaID,
		Categoria:              producto.Categoria,
		PrecioCentavos:         producto.PrecioCentavos,
		AnticipacionMinimaDias: producto.AnticipacionMinimaDias,
		AptoCeliacos:           producto.AptoCeliacos,
		Disponible:             producto.Disponible,
		ImagenURL:              producto.ImagenURL,
		CreadoEn:               producto.CreadoEn.UnixMilli(),
	}
}

func (doc documento) aDominio() dominio.Producto {
	return dominio.Producto{
		ID:                     doc.ID,
		Nombre:                 doc.Nombre,
		Descripcion:            doc.Descripcion,
		CategoriaID:            doc.CategoriaID,
		Categoria:              doc.Categoria,
		PrecioCentavos:         doc.PrecioCentavos,
		AnticipacionMinimaDias: doc.AnticipacionMinimaDias,
		AptoCeliacos:           doc.AptoCeliacos,
		Disponible:             doc.Disponible,
		ImagenURL:              doc.ImagenURL,
		CreadoEn:               time.UnixMilli(doc.CreadoEn).UTC(),
	}
}

// Buscador cumple puertos.Buscador.
type Buscador struct {
	cliente meili.ServiceManager
}

var _ puertos.Buscador = (*Buscador)(nil)

// Nuevo usa el Meilisearch de esa dirección (http://meilisearch:7700) con su clave maestra.
func Nuevo(direccion, clave string) *Buscador {
	return &Buscador{cliente: meili.New(direccion, meili.WithAPIKey(clave), meili.WithCustomClient(&http.Client{Timeout: tiempoDeIndexado}))}
}

// Preparar crea el índice si no existe y le aplica la configuración. Se puede
// correr en cada arranque.
func (buscador *Buscador) Preparar(contexto context.Context) error {
	return buscador.configurar(contexto, indiceVigente)
}

func (buscador *Buscador) configurar(contexto context.Context, uid string) error {
	contexto, cancelar := context.WithTimeout(contexto, tiempoDeIndexado)
	defer cancelar()
	// Actualizar la configuración de un índice que no existe lo crea.
	tarea, err := buscador.cliente.Index(uid).UpdateSettingsWithContext(contexto, configuracion)
	return buscador.esperar(contexto, tarea, err, "configurar el índice "+uid)
}

// esperar espera a que Meilisearch termine la tarea: sus operaciones son asíncronas.
func (buscador *Buscador) esperar(contexto context.Context, tarea *meili.TaskInfo, err error, que string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", que, err)
	}
	terminada, err := buscador.cliente.WaitForTaskWithContext(contexto, tarea.TaskUID, esperaEntreRevisiones)
	if err != nil {
		return fmt.Errorf("%s: esperar la tarea %d: %w", que, tarea.TaskUID, err)
	}
	if terminada.Status != meili.TaskStatusSucceeded {
		return fmt.Errorf("%s: la tarea %d terminó %s: %s", que, tarea.TaskUID, terminada.Status, terminada.Error.Message)
	}
	return nil
}

func (buscador *Buscador) Buscar(contexto context.Context, consulta dominio.Consulta) (dominio.PaginaDeProductos, error) {
	contexto, cancelar := context.WithTimeout(contexto, tiempoDeBusqueda)
	defer cancelar()
	porPagina := int64(dominio.TamanioPagina)
	respuesta, err := buscador.cliente.Index(indiceVigente).SearchWithContext(contexto, consulta.Texto, &meili.SearchRequest{
		Filter:      filtroDe(consulta),
		Sort:        ordenDe(consulta.Orden),
		Page:        int64(consulta.Pagina),
		HitsPerPage: &porPagina,
	})
	if err != nil {
		return dominio.PaginaDeProductos{}, fmt.Errorf("%w: %w", puertos.ErrBusquedaNoDisponible, err)
	}
	var documentos []documento
	if err := respuesta.Hits.DecodeInto(&documentos); err != nil {
		return dominio.PaginaDeProductos{}, fmt.Errorf("leer los resultados de la búsqueda: %w", err)
	}
	productos := make([]dominio.Producto, len(documentos))
	for posicion, doc := range documentos {
		productos[posicion] = doc.aDominio()
	}
	return dominio.PaginaDeProductos{Productos: productos, Pagina: consulta.Pagina, Total: int(respuesta.TotalHits)}, nil
}

// filtroDe arma el filtro de Meilisearch. Los textos van entre comillas y
// escapados: una categoría con comillas no puede cambiar el filtro.
func filtroDe(consulta dominio.Consulta) string {
	var condiciones []string
	if consulta.Disponible != nil {
		condiciones = append(condiciones, "disponible = "+strconv.FormatBool(*consulta.Disponible))
	}
	if consulta.Categoria != "" {
		condiciones = append(condiciones, "categoria = "+entreComillas(consulta.Categoria))
	}
	if consulta.PrecioMinimoCentavos != nil {
		condiciones = append(condiciones, "precioCentavos >= "+strconv.FormatInt(*consulta.PrecioMinimoCentavos, 10))
	}
	if consulta.PrecioMaximoCentavos != nil {
		condiciones = append(condiciones, "precioCentavos <= "+strconv.FormatInt(*consulta.PrecioMaximoCentavos, 10))
	}
	if consulta.AptoCeliacos != nil {
		condiciones = append(condiciones, "aptoCeliacos = "+strconv.FormatBool(*consulta.AptoCeliacos))
	}
	return strings.Join(condiciones, " AND ")
}

func entreComillas(texto string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(texto) + `"`
}

func ordenDe(orden dominio.Orden) []string {
	switch orden {
	case dominio.OrdenPrecioAsc:
		return []string{"precioCentavos:asc"}
	case dominio.OrdenPrecioDesc:
		return []string{"precioCentavos:desc"}
	case dominio.OrdenNovedad:
		return []string{"creadoEn:desc"}
	}
	return nil // relevancia
}

func (buscador *Buscador) Guardar(contexto context.Context, productos []dominio.Producto) error {
	return buscador.agregar(contexto, indiceVigente, productos)
}

func (buscador *Buscador) agregar(contexto context.Context, uid string, productos []dominio.Producto) error {
	contexto, cancelar := context.WithTimeout(contexto, tiempoDeIndexado)
	defer cancelar()
	documentos := make([]documento, len(productos))
	for posicion, producto := range productos {
		documentos[posicion] = documentoDe(producto)
	}
	clave := clavePrimaria
	tarea, err := buscador.cliente.Index(uid).AddDocumentsWithContext(contexto, documentos, &meili.DocumentOptions{PrimaryKey: &clave})
	return buscador.esperar(contexto, tarea, err, "guardar productos en "+uid)
}

func (buscador *Buscador) Borrar(contexto context.Context, ids []string) error {
	contexto, cancelar := context.WithTimeout(contexto, tiempoDeIndexado)
	defer cancelar()
	tarea, err := buscador.cliente.Index(indiceVigente).DeleteDocumentsWithContext(contexto, ids, nil)
	return buscador.esperar(contexto, tarea, err, "borrar productos del índice")
}

// IniciarReconstruccion crea un índice nuevo y vacío, con la misma configuración,
// al lado del vigente. Si quedó uno de una reconstrucción anterior, lo borra.
func (buscador *Buscador) IniciarReconstruccion(contexto context.Context) (puertos.Reconstruccion, error) {
	buscador.borrarIndice(contexto, indiceReconstruccion)
	if err := buscador.Preparar(contexto); err != nil { // el swap necesita que existan los dos
		return nil, err
	}
	if err := buscador.configurar(contexto, indiceReconstruccion); err != nil {
		return nil, err
	}
	return reconstruccion{buscador: buscador}, nil
}

func (buscador *Buscador) borrarIndice(contexto context.Context, uid string) error {
	contexto, cancelar := context.WithTimeout(contexto, tiempoDeIndexado)
	defer cancelar()
	tarea, err := buscador.cliente.DeleteIndexWithContext(contexto, uid)
	return buscador.esperar(contexto, tarea, err, "borrar el índice "+uid)
}

type reconstruccion struct {
	buscador *Buscador
}

func (actual reconstruccion) Agregar(contexto context.Context, productos []dominio.Producto) error {
	return actual.buscador.agregar(contexto, indiceReconstruccion, productos)
}

// Intercambiar deja el índice nuevo como vigente en una sola operación y borra el viejo.
func (actual reconstruccion) Intercambiar(contexto context.Context) error {
	contexto, cancelar := context.WithTimeout(contexto, tiempoDeIndexado)
	defer cancelar()
	tarea, err := actual.buscador.cliente.SwapIndexesWithContext(contexto, []*meili.SwapIndexesParams{{Indexes: []string{indiceVigente, indiceReconstruccion}}})
	if err := actual.buscador.esperar(contexto, tarea, err, "intercambiar los índices"); err != nil {
		return err
	}
	// Después del intercambio, el viejo quedó con el nombre de la reconstrucción.
	if err := actual.buscador.borrarIndice(contexto, indiceReconstruccion); err != nil {
		return errors.Join(errors.New("el índice nuevo ya está vigente, pero no se pudo borrar el viejo"), err)
	}
	return nil
}

func (actual reconstruccion) Descartar(contexto context.Context) {
	actual.buscador.borrarIndice(contexto, indiceReconstruccion)
}
