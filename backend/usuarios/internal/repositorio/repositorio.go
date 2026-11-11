// Package repositorio es la capa de acceso a MySQL de usuarios, con GORM
// (ADR-002, ADR-007). Las consultas viven acá y en ninguna otra capa.
package repositorio

import (
	"context"
	"errors"
	"fmt"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/modelo"
)

// Repositorio guarda y busca usuarios en MySQL.
type Repositorio struct {
	base *gorm.DB
}

// usuarioFila es la fila de la tabla usuarios. El email es único: MySQL rechaza
// en forma atómica una segunda cuenta con el mismo, aunque lleguen dos a la vez.
type usuarioFila struct {
	ID        string    `gorm:"primaryKey;size:36"`
	Email     string    `gorm:"size:254;not null;uniqueIndex"`
	Nombre    string    `gorm:"size:100;not null"`
	HashClave []byte    `gorm:"type:varbinary(60);not null"`
	Rol       string    `gorm:"size:20;not null"`
	CreadoEn  time.Time `gorm:"not null"`
}

func (usuarioFila) TableName() string { return "usuarios" }

func (fila usuarioFila) aModelo() modelo.Usuario {
	return modelo.Usuario{
		ID:        fila.ID,
		Email:     fila.Email,
		Nombre:    fila.Nombre,
		HashClave: fila.HashClave,
		Rol:       modelo.Rol(fila.Rol),
		CreadoEn:  fila.CreadoEn,
	}
}

// Abrir conecta con MySQL. El logger de GORM queda apagado: escribe texto suelto
// y rompería los logs en JSON (ADR-006); los errores vuelven por los métodos.
func Abrir(dsn string) (*Repositorio, error) {
	configuracion, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("MYSQL_DSN inválido: %w", err)
	}
	configuracion.ParseTime = true
	configuracion.Loc = time.UTC
	base, err := gorm.Open(gormmysql.New(gormmysql.Config{DSNConfig: configuracion}), &gorm.Config{TranslateError: true, Logger: logger.Discard})
	if err != nil {
		return nil, fmt.Errorf("conectar a MySQL: %w", err)
	}
	return &Repositorio{base: base}, nil
}

// Preparar crea o actualiza la tabla y su índice único.
func (repositorio *Repositorio) Preparar(contexto context.Context) error {
	if err := repositorio.base.WithContext(contexto).AutoMigrate(&usuarioFila{}); err != nil {
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

// Crear guarda un usuario nuevo. Devuelve modelo.ErrEmailRepetido si el email ya
// tiene cuenta.
func (repositorio *Repositorio) Crear(contexto context.Context, usuario modelo.Usuario) error {
	fila := usuarioFila{
		ID:        usuario.ID,
		Email:     usuario.Email,
		Nombre:    usuario.Nombre,
		HashClave: usuario.HashClave,
		Rol:       string(usuario.Rol),
		CreadoEn:  usuario.CreadoEn,
	}
	if err := repositorio.base.WithContext(contexto).Create(&fila).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return modelo.ErrEmailRepetido // el único índice único además del id
		}
		return fmt.Errorf("guardar el usuario: %w", err)
	}
	return nil
}

// BuscarPorEmail devuelve modelo.ErrNoEncontrado si no hay cuenta con ese email.
func (repositorio *Repositorio) BuscarPorEmail(contexto context.Context, email string) (modelo.Usuario, error) {
	return repositorio.buscar(contexto, "email = ?", email)
}

// BuscarPorID devuelve modelo.ErrNoEncontrado si el usuario no existe.
func (repositorio *Repositorio) BuscarPorID(contexto context.Context, id string) (modelo.Usuario, error) {
	return repositorio.buscar(contexto, "id = ?", id)
}

func (repositorio *Repositorio) buscar(contexto context.Context, condicion string, valor string) (modelo.Usuario, error) {
	var fila usuarioFila
	if err := repositorio.base.WithContext(contexto).Where(condicion, valor).Take(&fila).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return modelo.Usuario{}, modelo.ErrNoEncontrado
		}
		return modelo.Usuario{}, fmt.Errorf("buscar el usuario: %w", err)
	}
	return fila.aModelo(), nil
}
