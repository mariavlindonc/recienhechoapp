// Package mysql implementa con GORM los tres puertos de la base de productos:
// Categorias, Productos y BandejaDelIndice (ADR-002). Cada cambio de un producto
// guarda, en la misma transacción, su fila en la bandeja del índice (ADR-008).
package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
)

// Repositorio cumple puertos.Categorias, puertos.Productos y puertos.BandejaDelIndice.
type Repositorio struct {
	base  *gorm.DB
	ahora func() time.Time
}

var (
	_ puertos.Categorias       = (*Repositorio)(nil)
	_ puertos.Productos        = (*Repositorio)(nil)
	_ puertos.BandejaDelIndice = (*Repositorio)(nil)
)

// Abrir conecta con MySQL. El logger de GORM queda apagado: escribe texto suelto
// y rompería los logs en JSON (ADR-006); los errores vuelven por los métodos.
func Abrir(dsn string) (*gorm.DB, error) {
	configuracion, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("MYSQL_DSN inválido: %w", err)
	}
	// Sin esto, un UPDATE que deja el mismo valor dice "0 filas" y el producto
	// parecería no existir.
	configuracion.ClientFoundRows = true
	configuracion.ParseTime = true
	configuracion.Loc = time.UTC
	base, err := gorm.Open(gormmysql.New(gormmysql.Config{DSNConfig: configuracion}), &gorm.Config{TranslateError: true, Logger: logger.Discard})
	if err != nil {
		return nil, fmt.Errorf("conectar a MySQL: %w", err)
	}
	return base, nil
}

// NuevoRepositorio usa la base dada. ahora fecha las filas de la bandeja del índice.
func NuevoRepositorio(base *gorm.DB, ahora func() time.Time) *Repositorio {
	return &Repositorio{base: base, ahora: ahora}
}

// Preparar crea o actualiza las tablas y sus índices.
func (repositorio *Repositorio) Preparar(contexto context.Context) error {
	if err := repositorio.base.WithContext(contexto).AutoMigrate(&categoriaFila{}, &productoFila{}, &insumoFila{}, &cambioFila{}); err != nil {
		return fmt.Errorf("crear las tablas: %w", err)
	}
	return nil
}

// Ping dice si MySQL responde, para /health.
func (repositorio *Repositorio) Ping(contexto context.Context) error {
	conexion, err := repositorio.base.DB()
	if err != nil {
		return err
	}
	return conexion.PingContext(contexto)
}

func (repositorio *Repositorio) encolar(transaccion *gorm.DB, operacion puertos.OperacionDeIndice, productoIDs ...string) error {
	if len(productoIDs) == 0 {
		return nil
	}
	filas := make([]cambioFila, len(productoIDs))
	for posicion, id := range productoIDs {
		filas[posicion] = cambioFila{ProductoID: id, Operacion: string(operacion), CreadoEn: repositorio.ahora()}
	}
	if err := transaccion.Create(&filas).Error; err != nil {
		return fmt.Errorf("encolar en la bandeja del índice: %w", err)
	}
	return nil
}

// Categorías

func (repositorio *Repositorio) CrearCategoria(contexto context.Context, categoria dominio.Categoria) error {
	fila := categoriaFila{ID: categoria.ID, Nombre: categoria.Nombre, CreadoEn: categoria.CreadoEn}
	if err := repositorio.base.WithContext(contexto).Create(&fila).Error; err != nil {
		return traducir(err)
	}
	return nil
}

func (repositorio *Repositorio) BuscarCategoria(contexto context.Context, id string) (dominio.Categoria, error) {
	var fila categoriaFila
	if err := repositorio.base.WithContext(contexto).First(&fila, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dominio.Categoria{}, dominio.ErrCategoriaNoEncontrada
		}
		return dominio.Categoria{}, err
	}
	return fila.aDominio(), nil
}

func (repositorio *Repositorio) RenombrarCategoria(contexto context.Context, categoria dominio.Categoria) error {
	return repositorio.base.WithContext(contexto).Transaction(func(transaccion *gorm.DB) error {
		if err := transaccion.Model(&categoriaFila{}).Where("id = ?", categoria.ID).Update("nombre", categoria.Nombre).Error; err != nil {
			return traducir(err)
		}
		var productoIDs []string
		if err := transaccion.Model(&productoFila{}).Where("categoria_id = ?", categoria.ID).Pluck("id", &productoIDs).Error; err != nil {
			return err
		}
		return repositorio.encolar(transaccion, puertos.IndiceGuardar, productoIDs...)
	})
}

func (repositorio *Repositorio) EliminarCategoria(contexto context.Context, id string) error {
	return repositorio.base.WithContext(contexto).Transaction(func(transaccion *gorm.DB) error {
		// Bloquear la categoría primero: un producto que se crea en ella a la vez
		// espera, y después falla por la clave foránea en vez de quedar sin categoría.
		if err := transaccion.Clauses(clause.Locking{Strength: "UPDATE"}).First(&categoriaFila{}, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return dominio.ErrCategoriaNoEncontrada
			}
			return err
		}
		var activos int64
		if err := transaccion.Model(&productoFila{}).Where("categoria_id = ?", id).Count(&activos).Error; err != nil {
			return err
		}
		if activos > 0 {
			return dominio.ErrCategoriaEnUso
		}
		return transaccion.Delete(&categoriaFila{}, "id = ?", id).Error
	})
}

func (repositorio *Repositorio) ListarCategorias(contexto context.Context) ([]dominio.Categoria, error) {
	var filas []categoriaFila
	if err := repositorio.base.WithContext(contexto).Order("nombre").Find(&filas).Error; err != nil {
		return nil, err
	}
	categorias := make([]dominio.Categoria, len(filas))
	for posicion, fila := range filas {
		categorias[posicion] = fila.aDominio()
	}
	return categorias, nil
}

// Productos

// nombreDeCategoria completa el nombre de la categoría del producto, o devuelve
// dominio.ErrCategoriaNoEncontrada.
func nombreDeCategoria(transaccion *gorm.DB, producto dominio.Producto) (dominio.Producto, error) {
	var categoria categoriaFila
	if err := transaccion.First(&categoria, "id = ?", producto.CategoriaID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dominio.Producto{}, dominio.ErrCategoriaNoEncontrada
		}
		return dominio.Producto{}, err
	}
	producto.Categoria = categoria.Nombre
	return producto, nil
}

func (repositorio *Repositorio) CrearProducto(contexto context.Context, producto dominio.Producto) (dominio.Producto, error) {
	err := repositorio.base.WithContext(contexto).Transaction(func(transaccion *gorm.DB) error {
		var err error
		if producto, err = nombreDeCategoria(transaccion, producto); err != nil {
			return err
		}
		fila := filaDeProducto(producto)
		if err := transaccion.Omit("Categoria", "Receta").Create(&fila).Error; err != nil {
			return traducir(err)
		}
		return repositorio.encolar(transaccion, puertos.IndiceGuardar, producto.ID)
	})
	return producto, err
}

func (repositorio *Repositorio) ModificarProducto(contexto context.Context, producto dominio.Producto) (dominio.Producto, error) {
	err := repositorio.base.WithContext(contexto).Transaction(func(transaccion *gorm.DB) error {
		var err error
		if producto, err = nombreDeCategoria(transaccion, producto); err != nil {
			return err
		}
		// Un mapa, para que también se guarden los false y los ceros.
		resultado := transaccion.Model(&productoFila{}).Where("id = ?", producto.ID).Updates(map[string]any{
			"nombre":                   producto.Nombre,
			"descripcion":              producto.Descripcion,
			"categoria_id":             producto.CategoriaID,
			"precio_centavos":          producto.PrecioCentavos,
			"anticipacion_minima_dias": producto.AnticipacionMinimaDias,
			"apto_celiacos":            producto.AptoCeliacos,
			"disponible":               producto.Disponible,
			"actualizado_en":           producto.ActualizadoEn,
		})
		if err := sinFilaEsNoEncontrado(resultado); err != nil {
			return traducir(err)
		}
		return repositorio.encolar(transaccion, puertos.IndiceGuardar, producto.ID)
	})
	return producto, err
}

func (repositorio *Repositorio) GuardarImagen(contexto context.Context, producto dominio.Producto) error {
	return repositorio.base.WithContext(contexto).Transaction(func(transaccion *gorm.DB) error {
		resultado := transaccion.Model(&productoFila{}).Where("id = ?", producto.ID).
			Updates(map[string]any{"imagen_url": producto.ImagenURL, "actualizado_en": producto.ActualizadoEn})
		if err := sinFilaEsNoEncontrado(resultado); err != nil {
			return err
		}
		return repositorio.encolar(transaccion, puertos.IndiceGuardar, producto.ID)
	})
}

// GuardarReceta no pasa por la bandeja: la receta no está en el índice.
func (repositorio *Repositorio) GuardarReceta(contexto context.Context, producto dominio.Producto) error {
	return repositorio.base.WithContext(contexto).Transaction(func(transaccion *gorm.DB) error {
		resultado := transaccion.Model(&productoFila{}).Where("id = ?", producto.ID).Update("actualizado_en", producto.ActualizadoEn)
		if err := sinFilaEsNoEncontrado(resultado); err != nil {
			return err
		}
		if err := transaccion.Where("producto_id = ?", producto.ID).Delete(&insumoFila{}).Error; err != nil {
			return err
		}
		if len(producto.Receta) == 0 {
			return nil
		}
		filas := make([]insumoFila, len(producto.Receta))
		for posicion, insumo := range producto.Receta {
			filas[posicion] = insumoFila{ProductoID: producto.ID, Ingrediente: insumo.Ingrediente, Cantidad: insumo.Cantidad, Unidad: insumo.Unidad}
		}
		return transaccion.Create(&filas).Error
	})
}

func (repositorio *Repositorio) EliminarProducto(contexto context.Context, id string, ahora time.Time) error {
	return repositorio.base.WithContext(contexto).Transaction(func(transaccion *gorm.DB) error {
		// Update y no Delete, para fechar la baja con el reloj del servicio. GORM
		// agrega solo "eliminado_en IS NULL": dar de baja dos veces es "no encontrado".
		resultado := transaccion.Model(&productoFila{}).Where("id = ?", id).Update("eliminado_en", ahora)
		if err := sinFilaEsNoEncontrado(resultado); err != nil {
			return err
		}
		return repositorio.encolar(transaccion, puertos.IndiceBorrar, id)
	})
}

func (repositorio *Repositorio) BuscarProducto(contexto context.Context, id string) (dominio.Producto, error) {
	var fila productoFila
	err := repositorio.base.WithContext(contexto).
		Preload("Categoria").
		Preload("Receta", func(consulta *gorm.DB) *gorm.DB { return consulta.Order("id") }).
		First(&fila, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return dominio.Producto{}, dominio.ErrNoEncontrado
	}
	if err != nil {
		return dominio.Producto{}, err
	}
	return fila.aDominio(), nil
}

func (repositorio *Repositorio) ListarProductos(contexto context.Context, filtro dominio.FiltroDeCatalogo) (dominio.PaginaDeProductos, error) {
	consulta := repositorio.base.WithContext(contexto).Model(&productoFila{})
	if filtro.CategoriaID != "" {
		consulta = consulta.Where("categoria_id = ?", filtro.CategoriaID)
	}
	if !filtro.IncluirNoDisponibles {
		consulta = consulta.Where("disponible = ?", true)
	}
	var total int64
	if err := consulta.Count(&total).Error; err != nil {
		return dominio.PaginaDeProductos{}, err
	}
	var filas []productoFila
	err := consulta.Preload("Categoria").Order("nombre, id").
		Limit(dominio.TamanioPagina).Offset((filtro.Pagina - 1) * dominio.TamanioPagina).
		Find(&filas).Error
	if err != nil {
		return dominio.PaginaDeProductos{}, err
	}
	return dominio.PaginaDeProductos{Productos: productosADominio(filas), Pagina: filtro.Pagina, Total: int(total)}, nil
}

// Bandeja del índice

func (repositorio *Repositorio) CambiosPendientes(contexto context.Context, limite int) (puertos.CambiosDeIndice, error) {
	var filas []cambioFila
	if err := repositorio.base.WithContext(contexto).Order("id").Limit(limite).Find(&filas).Error; err != nil {
		return nil, err
	}
	cambios := make(puertos.CambiosDeIndice, len(filas))
	for posicion, fila := range filas {
		cambios[posicion] = fila.aPuerto()
	}
	return cambios, nil
}

func (repositorio *Repositorio) QuitarCambios(contexto context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return repositorio.base.WithContext(contexto).Delete(&cambioFila{}, ids).Error
}

func (repositorio *Repositorio) ProductosParaIndice(contexto context.Context, ids []string) ([]dominio.Producto, error) {
	var filas []productoFila
	if err := repositorio.base.WithContext(contexto).Preload("Categoria").Where("id IN ?", ids).Find(&filas).Error; err != nil {
		return nil, err
	}
	return productosADominio(filas), nil
}

func (repositorio *Repositorio) ListarParaReindexar(contexto context.Context, despuesDe string, limite int) ([]dominio.Producto, error) {
	var filas []productoFila
	err := repositorio.base.WithContext(contexto).Preload("Categoria").
		Where("id > ?", despuesDe).Order("id").Limit(limite).Find(&filas).Error
	if err != nil {
		return nil, err
	}
	return productosADominio(filas), nil
}

func sinFilaEsNoEncontrado(resultado *gorm.DB) error {
	if resultado.Error != nil {
		return resultado.Error
	}
	if resultado.RowsAffected == 0 {
		return dominio.ErrNoEncontrado
	}
	return nil
}

// traducir pasa los errores de restricciones de MySQL a errores del dominio.
func traducir(err error) error {
	switch {
	case errors.Is(err, gorm.ErrDuplicatedKey):
		return dominio.ErrCategoriaRepetida // el único índice único es el nombre de la categoría
	case errors.Is(err, gorm.ErrForeignKeyViolated):
		return dominio.ErrCategoriaNoEncontrada // la categoría se borró en el medio
	}
	return err
}
