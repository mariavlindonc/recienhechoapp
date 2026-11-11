package servicio

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/modelo"
)

var momento = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

// servicioSinBase sirve para lo que se valida antes de llegar al repositorio.
func servicioSinBase() *Servicio {
	return &Servicio{secreto: []byte(strings.Repeat("s", 32)), ahora: func() time.Time { return momento }, nuevoID: func() string { return "id-1" }}
}

func TestRegistrarRechazaDatosInvalidosSinTocarLaBase(prueba *testing.T) {
	casos := map[string]DatosRegistro{
		"email sin arroba":     {Email: "ana.ejemplo.com", Nombre: "Ana", Clave: "clave-segura"},
		"email con nombre":     {Email: "Ana <ana@ejemplo.com>", Nombre: "Ana", Clave: "clave-segura"},
		"nombre vacío":         {Email: "ana@ejemplo.com", Nombre: "   ", Clave: "clave-segura"},
		"clave corta":          {Email: "ana@ejemplo.com", Nombre: "Ana", Clave: "corta"},
		"clave de más de 72 B": {Email: "ana@ejemplo.com", Nombre: "Ana", Clave: strings.Repeat("x", 73)},
	}
	for nombre, datos := range casos {
		prueba.Run(nombre, func(prueba *testing.T) {
			_, err := servicioSinBase().Registrar(context.Background(), datos)

			if !errors.Is(err, modelo.ErrSolicitudInvalida) {
				prueba.Errorf("error = %v, se esperaba ErrSolicitudInvalida", err)
			}
		})
	}
}

func TestNormalizarEmailPasaAMinusculas(prueba *testing.T) {
	email, err := normalizarEmail("  Ana@Ejemplo.COM ")

	if err != nil || email != "ana@ejemplo.com" {
		prueba.Errorf("normalizarEmail = %q, %v", email, err)
	}
}

func TestElTokenLlevaLaIdentidadYVenceEnUnaHora(prueba *testing.T) {
	servicio := servicioSinBase()
	usuario := modelo.Usuario{ID: "id-ana", Email: "ana@ejemplo.com", Rol: modelo.RolComprador}

	token, err := servicio.firmarToken(usuario, momento.Add(VidaDelToken))
	if err != nil {
		prueba.Fatalf("firmar: %v", err)
	}

	var afirmaciones Afirmaciones
	_, err = jwt.ParseWithClaims(token, &afirmaciones, func(*jwt.Token) (any, error) { return servicio.secreto, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithTimeFunc(func() time.Time { return momento }))
	if err != nil {
		prueba.Fatalf("verificar: %v", err)
	}
	if afirmaciones.Subject != "id-ana" || afirmaciones.Email != "ana@ejemplo.com" || afirmaciones.Rol != "comprador" {
		prueba.Errorf("afirmaciones = %+v", afirmaciones)
	}
	if !afirmaciones.ExpiresAt.Time.Equal(momento.Add(time.Hour)) {
		prueba.Errorf("vence = %v, se esperaba una hora después", afirmaciones.ExpiresAt)
	}
}

func TestUnTokenFirmadoConOtroSecretoNoVale(prueba *testing.T) {
	token, _ := servicioSinBase().firmarToken(modelo.Usuario{ID: "id-ana"}, momento.Add(VidaDelToken))

	_, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return []byte(strings.Repeat("otro", 8)), nil },
		jwt.WithTimeFunc(func() time.Time { return momento }))

	if err == nil {
		prueba.Error("un token con otro secreto tiene que rechazarse")
	}
}
