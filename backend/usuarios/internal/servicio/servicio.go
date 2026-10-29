// Package servicio es la capa de reglas de usuarios (ADR-007): valida los datos,
// hashea las contraseñas con bcrypt y firma el token de sesión (ADR-016). Solo
// llama a la capa de abajo, el repositorio.
package servicio

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/modelo"
	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/repositorio"
)

const (
	// VidaDelToken es cuánto dura una sesión (ADR-016): un token no se puede
	// revocar, así que la vida corta acota el daño si se filtra.
	VidaDelToken = time.Hour

	largoMinimoClave  = 8
	largoMaximoClave  = 72 // bcrypt ignora lo que pase de 72 bytes
	largoMaximoEmail  = 254
	largoMaximoNombre = 100
)

// Servicio atiende el registro, el inicio de sesión y el perfil.
type Servicio struct {
	repositorio *repositorio.Repositorio
	secreto     []byte
	ahora       func() time.Time
	nuevoID     func() string
	// hashDeRelleno se compara cuando el email no existe, para que la respuesta
	// tarde lo mismo y no delate qué emails tienen cuenta.
	hashDeRelleno []byte
}

// Nuevo arma el servicio. secreto firma los tokens y lo comparte el api-gateway,
// que los verifica.
func Nuevo(repositorio *repositorio.Repositorio, secreto []byte, ahora func() time.Time, nuevoID func() string) (*Servicio, error) {
	hashDeRelleno, err := bcrypt.GenerateFromPassword([]byte("relleno-para-emails-sin-cuenta"), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("preparar el hash de relleno: %w", err)
	}
	return &Servicio{repositorio: repositorio, secreto: secreto, ahora: ahora, nuevoID: nuevoID, hashDeRelleno: hashDeRelleno}, nil
}

// DatosRegistro es lo que manda quien crea una cuenta.
type DatosRegistro struct {
	Email  string
	Nombre string
	Clave  string
}

// Sesion es el resultado de iniciar sesión.
type Sesion struct {
	Token   string
	VenceEn time.Time
	Usuario modelo.Usuario
}

// Registrar crea una cuenta de comprador. Los vendedores no se registran: la
// cuenta sale del .env al arrancar (AsegurarVendedor).
func (servicio *Servicio) Registrar(contexto context.Context, datos DatosRegistro) (modelo.Usuario, error) {
	return servicio.crear(contexto, datos, modelo.RolComprador)
}

// AsegurarVendedor crea la cuenta del vendedor si todavía no existe. Si ya existe
// no la toca: cambiar la clave en el .env no cambia la de una cuenta creada. Si el
// email es de un comprador, falla: arrancar así dejaría al sistema sin vendedor.
func (servicio *Servicio) AsegurarVendedor(contexto context.Context, email, clave string) error {
	_, err := servicio.crear(contexto, DatosRegistro{Email: email, Nombre: "Vendedor", Clave: clave}, modelo.RolVendedor)
	if errors.Is(err, modelo.ErrEmailRepetido) {
		normalizado, _ := normalizarEmail(email) // crear ya lo validó
		existente, errBusqueda := servicio.repositorio.BuscarPorEmail(contexto, normalizado)
		if errBusqueda != nil {
			return fmt.Errorf("revisar la cuenta del vendedor: %w", errBusqueda)
		}
		if existente.Rol != modelo.RolVendedor {
			return fmt.Errorf("VENDEDOR_EMAIL es de una cuenta con rol %s: elegí otro email para el vendedor", existente.Rol)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("crear la cuenta del vendedor: %w", err)
	}
	return nil
}

func (servicio *Servicio) crear(contexto context.Context, datos DatosRegistro, rol modelo.Rol) (modelo.Usuario, error) {
	email, err := normalizarEmail(datos.Email)
	if err != nil {
		return modelo.Usuario{}, err
	}
	nombre := strings.TrimSpace(datos.Nombre)
	if nombre == "" || utf8.RuneCountInString(nombre) > largoMaximoNombre {
		return modelo.Usuario{}, fmt.Errorf("%w: el nombre tiene que tener entre 1 y %d caracteres", modelo.ErrSolicitudInvalida, largoMaximoNombre)
	}
	if len(datos.Clave) < largoMinimoClave || len(datos.Clave) > largoMaximoClave {
		return modelo.Usuario{}, fmt.Errorf("%w: la contraseña tiene que tener entre %d y %d bytes", modelo.ErrSolicitudInvalida, largoMinimoClave, largoMaximoClave)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(datos.Clave), bcrypt.DefaultCost)
	if err != nil {
		return modelo.Usuario{}, fmt.Errorf("hashear la contraseña: %w", err)
	}

	usuario := modelo.Usuario{ID: servicio.nuevoID(), Email: email, Nombre: nombre, HashClave: hash, Rol: rol, CreadoEn: servicio.ahora()}
	if err := servicio.repositorio.Crear(contexto, usuario); err != nil {
		return modelo.Usuario{}, err
	}
	return usuario, nil
}

// IniciarSesion verifica email y contraseña y devuelve un token firmado. Ante un
// email inexistente o una contraseña mal responde lo mismo, sin decir cuál falló.
func (servicio *Servicio) IniciarSesion(contexto context.Context, email, clave string) (Sesion, error) {
	email, err := normalizarEmail(email)
	if err != nil {
		return Sesion{}, modelo.ErrCredencialesInvalidas
	}
	usuario, err := servicio.repositorio.BuscarPorEmail(contexto, email)
	if errors.Is(err, modelo.ErrNoEncontrado) {
		bcrypt.CompareHashAndPassword(servicio.hashDeRelleno, []byte(clave))
		return Sesion{}, modelo.ErrCredencialesInvalidas
	}
	if err != nil {
		return Sesion{}, err
	}
	if bcrypt.CompareHashAndPassword(usuario.HashClave, []byte(clave)) != nil {
		return Sesion{}, modelo.ErrCredencialesInvalidas
	}

	venceEn := servicio.ahora().Add(VidaDelToken)
	token, err := servicio.firmarToken(usuario, venceEn)
	if err != nil {
		return Sesion{}, err
	}
	return Sesion{Token: token, VenceEn: venceEn, Usuario: usuario}, nil
}

// Perfil devuelve los datos de un usuario.
func (servicio *Servicio) Perfil(contexto context.Context, id string) (modelo.Usuario, error) {
	return servicio.repositorio.BuscarPorID(contexto, id)
}

// Ping dice si la base responde, para /health.
func (servicio *Servicio) Ping(contexto context.Context) error {
	return servicio.repositorio.Ping(contexto)
}

// Afirmaciones son los datos del token. El api-gateway los convierte en los
// headers X-Id-Usuario, X-Email-Usuario y X-Rol-Usuario (ADR-016).
type Afirmaciones struct {
	Email string `json:"email"`
	Rol   string `json:"rol"`
	jwt.RegisteredClaims
}

// firmarToken arma un JWT HS256 con el id en sub.
func (servicio *Servicio) firmarToken(usuario modelo.Usuario, venceEn time.Time) (string, error) {
	afirmaciones := Afirmaciones{
		Email: usuario.Email,
		Rol:   string(usuario.Rol),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   usuario.ID,
			IssuedAt:  jwt.NewNumericDate(servicio.ahora()),
			ExpiresAt: jwt.NewNumericDate(venceEn),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, afirmaciones).SignedString(servicio.secreto)
	if err != nil {
		return "", fmt.Errorf("firmar el token: %w", err)
	}
	return token, nil
}

// normalizarEmail acepta solo una dirección suelta ("ana@ejemplo.com", sin
// nombre ni <>) y la pasa a minúsculas, así "Ana@" y "ana@" son la misma cuenta.
func normalizarEmail(texto string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(texto))
	direccion, err := mail.ParseAddress(email)
	if err != nil || direccion.Address != email || len(email) > largoMaximoEmail {
		return "", fmt.Errorf("%w: el email no es válido", modelo.ErrSolicitudInvalida)
	}
	return email, nil
}
