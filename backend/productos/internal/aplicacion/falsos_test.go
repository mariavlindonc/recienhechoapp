package aplicacion_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/aplicacion"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
)

// baseFalsa cumple los tres puertos de la base (Categorias, Productos y
// BandejaDelIndice) en memoria, como el adaptador MySQL los cumple con una sola base.
type baseFalsa struct {
	ahora          func() time.Time
	categorias     map[string]dominio.Categoria
	productos      map[string]dominio.Producto
	bandeja        puertos.CambiosDeIndice
	ultimaFila     int64
	lecturas       int
	fallarImagen   error
	fallarProducto error
}

func (base *baseFalsa) encolar(productoID string, operacion puertos.OperacionDeIndice) {
	base.ultimaFila++
	base.bandeja = append(base.bandeja, puertos.CambioDeIndice{ID: base.ultimaFila, ProductoID: productoID, Operacion: operacion, CreadoEn: base.ahora()})
}

func (base *baseFalsa) nombreRepetido(nombre, salvoID string) bool {
	for _, categoria := range base.categorias {
		if categoria.ID != salvoID && strings.EqualFold(categoria.Nombre, nombre) {
			return true
		}
	}
	return false
}

func (base *baseFalsa) CrearCategoria(_ context.Context, categoria dominio.Categoria) error {
	if base.nombreRepetido(categoria.Nombre, "") {
		return dominio.ErrCategoriaRepetida
	}
	base.categorias[categoria.ID] = categoria
	return nil
}

func (base *baseFalsa) BuscarCategoria(_ context.Context, id string) (dominio.Categoria, error) {
	categoria, existe := base.categorias[id]
	if !existe {
		return dominio.Categoria{}, dominio.ErrCategoriaNoEncontrada
	}
	return categoria, nil
}

func (base *baseFalsa) RenombrarCategoria(_ context.Context, categoria dominio.Categoria) error {
	if base.nombreRepetido(categoria.Nombre, categoria.ID) {
		return dominio.ErrCategoriaRepetida
	}
	base.categorias[categoria.ID] = categoria
	for id, producto := range base.productos {
		if producto.CategoriaID == categoria.ID {
			producto.Categoria = categoria.Nombre
			base.productos[id] = producto
			base.encolar(id, puertos.IndiceGuardar)
		}
	}
	return nil
}

func (base *baseFalsa) EliminarCategoria(_ context.Context, id string) error {
	if _, existe := base.categorias[id]; !existe {
		return dominio.ErrCategoriaNoEncontrada
	}
	for _, producto := range base.productos {
		if producto.CategoriaID == id {
			return dominio.ErrCategoriaEnUso
		}
	}
	delete(base.categorias, id)
	return nil
}

func (base *baseFalsa) ListarCategorias(context.Context) ([]dominio.Categoria, error) {
	base.lecturas++
	var lista []dominio.Categoria
	for _, categoria := range base.categorias {
		lista = append(lista, categoria)
	}
	slices.SortFunc(lista, func(una, otra dominio.Categoria) int { return strings.Compare(una.Nombre, otra.Nombre) })
	return lista, nil
}

func (base *baseFalsa) conCategoria(producto dominio.Producto) (dominio.Producto, error) {
	categoria, existe := base.categorias[producto.CategoriaID]
	if !existe {
		return dominio.Producto{}, dominio.ErrCategoriaNoEncontrada
	}
	producto.Categoria = categoria.Nombre
	return producto, nil
}

func (base *baseFalsa) CrearProducto(_ context.Context, producto dominio.Producto) (dominio.Producto, error) {
	producto, err := base.conCategoria(producto)
	if err != nil {
		return dominio.Producto{}, err
	}
	base.productos[producto.ID] = producto
	base.encolar(producto.ID, puertos.IndiceGuardar)
	return producto, nil
}

func (base *baseFalsa) ModificarProducto(_ context.Context, producto dominio.Producto) (dominio.Producto, error) {
	if _, existe := base.productos[producto.ID]; !existe {
		return dominio.Producto{}, dominio.ErrNoEncontrado
	}
	producto, err := base.conCategoria(producto)
	if err != nil {
		return dominio.Producto{}, err
	}
	base.productos[producto.ID] = producto
	base.encolar(producto.ID, puertos.IndiceGuardar)
	return producto, nil
}

func (base *baseFalsa) GuardarImagen(_ context.Context, producto dominio.Producto) error {
	if base.fallarImagen != nil {
		return base.fallarImagen
	}
	guardado := base.productos[producto.ID]
	guardado.ImagenURL = producto.ImagenURL
	base.productos[producto.ID] = guardado
	base.encolar(producto.ID, puertos.IndiceGuardar)
	return nil
}

func (base *baseFalsa) GuardarReceta(_ context.Context, producto dominio.Producto) error {
	guardado := base.productos[producto.ID]
	guardado.Receta = producto.Receta
	base.productos[producto.ID] = guardado
	return nil
}

func (base *baseFalsa) EliminarProducto(_ context.Context, id string, _ time.Time) error {
	if _, existe := base.productos[id]; !existe {
		return dominio.ErrNoEncontrado
	}
	delete(base.productos, id)
	base.encolar(id, puertos.IndiceBorrar)
	return nil
}

func (base *baseFalsa) BuscarProducto(_ context.Context, id string) (dominio.Producto, error) {
	base.lecturas++
	if base.fallarProducto != nil {
		return dominio.Producto{}, base.fallarProducto
	}
	producto, existe := base.productos[id]
	if !existe {
		return dominio.Producto{}, dominio.ErrNoEncontrado
	}
	return producto, nil
}

func (base *baseFalsa) ListarProductos(_ context.Context, filtro dominio.FiltroDeCatalogo) (dominio.PaginaDeProductos, error) {
	base.lecturas++
	var lista []dominio.Producto
	for _, producto := range base.productos {
		if (filtro.CategoriaID == "" || producto.CategoriaID == filtro.CategoriaID) && (producto.Disponible || filtro.IncluirNoDisponibles) {
			lista = append(lista, producto.VistaPublica())
		}
	}
	slices.SortFunc(lista, func(uno, otro dominio.Producto) int { return strings.Compare(uno.Nombre, otro.Nombre) })
	return dominio.PaginaDeProductos{Productos: lista, Pagina: filtro.Pagina, Total: len(lista)}, nil
}

func (base *baseFalsa) CambiosPendientes(_ context.Context, limite int) (puertos.CambiosDeIndice, error) {
	return slices.Clone(base.bandeja[:min(limite, len(base.bandeja))]), nil
}

func (base *baseFalsa) QuitarCambios(_ context.Context, ids []int64) error {
	base.bandeja = slices.DeleteFunc(base.bandeja, func(cambio puertos.CambioDeIndice) bool { return slices.Contains(ids, cambio.ID) })
	return nil
}

func (base *baseFalsa) ProductosParaIndice(_ context.Context, ids []string) ([]dominio.Producto, error) {
	var encontrados []dominio.Producto
	for _, id := range ids {
		if producto, existe := base.productos[id]; existe {
			encontrados = append(encontrados, producto.VistaPublica())
		}
	}
	return encontrados, nil
}

func (base *baseFalsa) ListarParaReindexar(_ context.Context, despuesDe string, limite int) ([]dominio.Producto, error) {
	var ids []string
	for id := range base.productos {
		if id > despuesDe {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	var lote []dominio.Producto
	for _, id := range ids[:min(limite, len(ids))] {
		lote = append(lote, base.productos[id].VistaPublica())
	}
	return lote, nil
}

// buscadorFalso guarda el índice en memoria.
type buscadorFalso struct {
	indice          map[string]dominio.Producto
	caido           bool
	consulta        dominio.Consulta
	reconstruccion  []dominio.Producto
	intercambiado   bool
	descartado      bool
	fallarAlAgregar bool
}

var errMotorCaido = errors.New("Meilisearch no responde")

func (buscador *buscadorFalso) Buscar(_ context.Context, consulta dominio.Consulta) (dominio.PaginaDeProductos, error) {
	if buscador.caido {
		return dominio.PaginaDeProductos{}, fmt.Errorf("%w: %w", puertos.ErrBusquedaNoDisponible, errMotorCaido)
	}
	buscador.consulta = consulta
	return dominio.PaginaDeProductos{Pagina: consulta.Pagina, Total: len(buscador.indice)}, nil
}

func (buscador *buscadorFalso) Guardar(_ context.Context, productos []dominio.Producto) error {
	if buscador.caido {
		return errMotorCaido
	}
	for _, producto := range productos {
		buscador.indice[producto.ID] = producto
	}
	return nil
}

func (buscador *buscadorFalso) Borrar(_ context.Context, ids []string) error {
	if buscador.caido {
		return errMotorCaido
	}
	for _, id := range ids {
		delete(buscador.indice, id)
	}
	return nil
}

func (buscador *buscadorFalso) IniciarReconstruccion(context.Context) (puertos.Reconstruccion, error) {
	buscador.reconstruccion = nil
	return buscador, nil
}

func (buscador *buscadorFalso) Agregar(_ context.Context, productos []dominio.Producto) error {
	if buscador.fallarAlAgregar {
		return errMotorCaido
	}
	buscador.reconstruccion = append(buscador.reconstruccion, productos...)
	return nil
}

func (buscador *buscadorFalso) Intercambiar(context.Context) error {
	buscador.indice = map[string]dominio.Producto{}
	for _, producto := range buscador.reconstruccion {
		buscador.indice[producto.ID] = producto
	}
	buscador.intercambiado = true
	return nil
}

func (buscador *buscadorFalso) Descartar(context.Context) { buscador.descartado = true }

// cacheFalsa guarda en mapas y puede simular que Redis no responde.
type cacheFalsa struct {
	productos     map[string]dominio.Producto
	paginas       map[dominio.FiltroDeCatalogo]dominio.PaginaDeProductos
	categorias    []dominio.Categoria
	hayCategorias bool
	caida         bool
	escrituras    int
}

var errRedisCaido = errors.New("Redis no responde")

func (cache *cacheFalsa) Producto(_ context.Context, id string) (dominio.Producto, bool, error) {
	if cache.caida {
		return dominio.Producto{}, false, errRedisCaido
	}
	producto, existe := cache.productos[id]
	return producto, existe, nil
}

func (cache *cacheFalsa) GuardarProducto(_ context.Context, producto dominio.Producto) error {
	if cache.caida {
		return errRedisCaido
	}
	cache.escrituras++
	cache.productos[producto.ID] = producto
	return nil
}

func (cache *cacheFalsa) Pagina(_ context.Context, filtro dominio.FiltroDeCatalogo) (dominio.PaginaDeProductos, bool, error) {
	if cache.caida {
		return dominio.PaginaDeProductos{}, false, errRedisCaido
	}
	pagina, existe := cache.paginas[filtro]
	return pagina, existe, nil
}

func (cache *cacheFalsa) GuardarPagina(_ context.Context, filtro dominio.FiltroDeCatalogo, pagina dominio.PaginaDeProductos) error {
	if cache.caida {
		return errRedisCaido
	}
	cache.escrituras++
	cache.paginas[filtro] = pagina
	return nil
}

func (cache *cacheFalsa) Categorias(context.Context) ([]dominio.Categoria, bool, error) {
	if cache.caida {
		return nil, false, errRedisCaido
	}
	return cache.categorias, cache.hayCategorias, nil
}

func (cache *cacheFalsa) GuardarCategorias(_ context.Context, categorias []dominio.Categoria) error {
	if cache.caida {
		return errRedisCaido
	}
	cache.escrituras++
	cache.categorias, cache.hayCategorias = categorias, true
	return nil
}

func (cache *cacheFalsa) InvalidarProducto(_ context.Context, id string) error {
	if cache.caida {
		return errRedisCaido
	}
	delete(cache.productos, id)
	return nil
}

func (cache *cacheFalsa) InvalidarCatalogo(context.Context) error {
	if cache.caida {
		return errRedisCaido
	}
	cache.paginas = map[dominio.FiltroDeCatalogo]dominio.PaginaDeProductos{}
	cache.categorias, cache.hayCategorias = nil, false
	return nil
}

// almacenFalso guarda los archivos en un mapa por URL.
type almacenFalso struct {
	archivos map[string][]byte
	cantidad int
}

func (almacen *almacenFalso) Guardar(_ context.Context, imagen dominio.Imagen) (string, error) {
	almacen.cantidad++
	url := fmt.Sprintf("/imagenes/archivo-%d%s", almacen.cantidad, imagen.Extension)
	almacen.archivos[url] = imagen.Contenido
	return url, nil
}

func (almacen *almacenFalso) Borrar(_ context.Context, url string) error {
	delete(almacen.archivos, url)
	return nil
}

// eventosFalsos anota lo que avisó la aplicación.
type eventosFalsos struct {
	aciertos, fallos map[string]int
	fallosDeCache    []string
	sincronizados    int
	errIndice        error
	antiguedad       time.Duration
}

func (eventos *eventosFalsos) CacheConsultada(_ context.Context, tipo string, acierto bool) {
	if acierto {
		eventos.aciertos[tipo]++
	} else {
		eventos.fallos[tipo]++
	}
}

func (eventos *eventosFalsos) CacheFallo(_ context.Context, operacion string, _ error) {
	eventos.fallosDeCache = append(eventos.fallosDeCache, operacion)
}

func (eventos *eventosFalsos) IndiceSincronizado(_ context.Context, cantidad int, err error) {
	eventos.sincronizados += cantidad
	eventos.errIndice = err
}

func (eventos *eventosFalsos) AntiguedadDelIndice(_ context.Context, antiguedad time.Duration) {
	eventos.antiguedad = antiguedad
}

func (eventos *eventosFalsos) ImagenSinBorrar(context.Context, string, error) {}

// entorno es el servicio armado con todos los puertos falsos y un reloj que el test mueve.
type entorno struct {
	servicio *aplicacion.Servicio
	base     *baseFalsa
	buscador *buscadorFalso
	cache    *cacheFalsa
	almacen  *almacenFalso
	eventos  *eventosFalsos
	ahora    time.Time
}

func armar() *entorno {
	actual := &entorno{ahora: time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)}
	reloj := func() time.Time { return actual.ahora }
	actual.base = &baseFalsa{ahora: reloj, categorias: map[string]dominio.Categoria{}, productos: map[string]dominio.Producto{}}
	actual.buscador = &buscadorFalso{indice: map[string]dominio.Producto{}}
	actual.cache = &cacheFalsa{productos: map[string]dominio.Producto{}, paginas: map[dominio.FiltroDeCatalogo]dominio.PaginaDeProductos{}}
	actual.almacen = &almacenFalso{archivos: map[string][]byte{}}
	actual.eventos = &eventosFalsos{aciertos: map[string]int{}, fallos: map[string]int{}}
	ids := 0
	actual.servicio = aplicacion.NuevoServicio(aplicacion.Dependencias{
		Categorias: actual.base,
		Productos:  actual.base,
		Bandeja:    actual.base,
		Buscador:   actual.buscador,
		Cache:      actual.cache,
		Imagenes:   actual.almacen,
		Eventos:    actual.eventos,
		Ahora:      reloj,
		NuevoID: func() string {
			ids++
			return fmt.Sprintf("id-%02d", ids)
		},
	})
	return actual
}
