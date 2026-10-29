package mysql

import (
	"time"

	"gorm.io/gorm"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
)

// Filas de las tablas. Los tags de GORM viven solo acá: el dominio no los conoce
// (ADR-007). Las tablas las crea AutoMigrate al arrancar.

// categoriaFila no tiene baja lógica: con baja lógica, el nombre de una categoría
// borrada seguiría ocupando el índice único. La comparación de nombres ignora
// mayúsculas y tildes por la collation de MySQL 8 (utf8mb4_0900_ai_ci).
type categoriaFila struct {
	ID       string    `gorm:"primaryKey;size:36"`
	Nombre   string    `gorm:"size:50;not null;uniqueIndex:categoria_nombre"`
	CreadoEn time.Time `gorm:"not null"`
}

func (categoriaFila) TableName() string { return "categorias" }

// productoFila tiene baja lógica (eliminado_en). categoria_id queda en NULL solo
// en productos dados de baja cuya categoría se borró después.
type productoFila struct {
	ID                     string         `gorm:"primaryKey;size:36"`
	Nombre                 string         `gorm:"size:100;not null;index:catalogo,priority:2"`
	Descripcion            string         `gorm:"size:1000;not null"`
	CategoriaID            *string        `gorm:"size:36;index:catalogo,priority:1"`
	Categoria              *categoriaFila `gorm:"foreignKey:CategoriaID;constraint:OnDelete:SET NULL"`
	PrecioCentavos         int64          `gorm:"not null"`
	AnticipacionMinimaDias int            `gorm:"not null"`
	AptoCeliacos           bool           `gorm:"not null"`
	Disponible             bool           `gorm:"not null"`
	ImagenURL              string         `gorm:"size:200;not null"`
	Receta                 []insumoFila   `gorm:"foreignKey:ProductoID;constraint:OnDelete:CASCADE"`
	CreadoEn               time.Time      `gorm:"not null"`
	ActualizadoEn          time.Time      `gorm:"not null"`
	EliminadoEn            gorm.DeletedAt `gorm:"index"`
}

func (productoFila) TableName() string { return "productos" }

type insumoFila struct {
	ID          uint64 `gorm:"primaryKey"`
	ProductoID  string `gorm:"size:36;not null;index"`
	Ingrediente string `gorm:"size:100;not null"`
	Cantidad    int64  `gorm:"not null"`
	Unidad      string `gorm:"size:10;not null"`
}

func (insumoFila) TableName() string { return "receta_insumos" }

// cambioFila es una fila de la bandeja de salida del índice (ADR-008).
type cambioFila struct {
	ID         int64     `gorm:"primaryKey;autoIncrement"`
	ProductoID string    `gorm:"size:36;not null"`
	Operacion  string    `gorm:"size:10;not null"`
	CreadoEn   time.Time `gorm:"not null"`
}

func (cambioFila) TableName() string { return "indice_pendiente" }

func (fila categoriaFila) aDominio() dominio.Categoria {
	return dominio.Categoria{ID: fila.ID, Nombre: fila.Nombre, CreadoEn: fila.CreadoEn}
}

func (fila productoFila) aDominio() dominio.Producto {
	producto := dominio.Producto{
		ID:                     fila.ID,
		Nombre:                 fila.Nombre,
		Descripcion:            fila.Descripcion,
		PrecioCentavos:         fila.PrecioCentavos,
		AnticipacionMinimaDias: fila.AnticipacionMinimaDias,
		AptoCeliacos:           fila.AptoCeliacos,
		Disponible:             fila.Disponible,
		ImagenURL:              fila.ImagenURL,
		CreadoEn:               fila.CreadoEn,
		ActualizadoEn:          fila.ActualizadoEn,
	}
	if fila.CategoriaID != nil {
		producto.CategoriaID = *fila.CategoriaID
	}
	if fila.Categoria != nil {
		producto.Categoria = fila.Categoria.Nombre
	}
	for _, insumo := range fila.Receta {
		producto.Receta = append(producto.Receta, dominio.Insumo{Ingrediente: insumo.Ingrediente, Cantidad: insumo.Cantidad, Unidad: insumo.Unidad})
	}
	return producto
}

func filaDeProducto(producto dominio.Producto) productoFila {
	return productoFila{
		ID:                     producto.ID,
		Nombre:                 producto.Nombre,
		Descripcion:            producto.Descripcion,
		CategoriaID:            &producto.CategoriaID,
		PrecioCentavos:         producto.PrecioCentavos,
		AnticipacionMinimaDias: producto.AnticipacionMinimaDias,
		AptoCeliacos:           producto.AptoCeliacos,
		Disponible:             producto.Disponible,
		ImagenURL:              producto.ImagenURL,
		CreadoEn:               producto.CreadoEn,
		ActualizadoEn:          producto.ActualizadoEn,
	}
}

func productosADominio(filas []productoFila) []dominio.Producto {
	productos := make([]dominio.Producto, len(filas))
	for posicion, fila := range filas {
		productos[posicion] = fila.aDominio()
	}
	return productos
}

func (fila cambioFila) aPuerto() puertos.CambioDeIndice {
	return puertos.CambioDeIndice{ID: fila.ID, ProductoID: fila.ProductoID, Operacion: puertos.OperacionDeIndice(fila.Operacion), CreadoEn: fila.CreadoEn}
}
