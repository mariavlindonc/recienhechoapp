// Package modelo tiene los datos y errores que comparten las capas de usuarios
// (ADR-007): handler → servicio → repositorio.
package modelo

import (
	"errors"
	"time"
)

// Rol define qué puede hacer un usuario.
type Rol string

const (
	RolComprador Rol = "comprador"
	RolVendedor  Rol = "vendedor"
)

// Usuario es una cuenta. HashClave es el bcrypt de la contraseña: nunca se
// devuelve en una respuesta (RNF8).
type Usuario struct {
	ID        string
	Email     string
	Nombre    string
	HashClave []byte
	Rol       Rol
	CreadoEn  time.Time
}

var (
	ErrSolicitudInvalida     = errors.New("solicitud inválida")
	ErrEmailRepetido         = errors.New("ya existe una cuenta con ese email")
	ErrCredencialesInvalidas = errors.New("email o contraseña incorrectos")
	ErrNoEncontrado          = errors.New("usuario inexistente")
)
