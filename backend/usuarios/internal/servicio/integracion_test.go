package servicio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/modelo"
	"github.com/mariavlindonc/recienhechoapp/backend/usuarios/internal/repositorio"
)

// servicioConMySQL usa un MySQL real. Se activa con MYSQL_DSN_TEST (por ejemplo,
// usuarios:desarrollo@tcp(127.0.0.1:3308)/usuarios) y se saltea si no está.
func servicioConMySQL(prueba *testing.T) *Servicio {
	prueba.Helper()
	dsn := os.Getenv("MYSQL_DSN_TEST")
	if dsn == "" {
		prueba.Skip("MYSQL_DSN_TEST no está definida")
	}
	repositorioUsuarios, err := repositorio.Abrir(dsn)
	if err != nil {
		prueba.Fatalf("abrir MySQL: %v", err)
	}
	if err := repositorioUsuarios.Preparar(context.Background()); err != nil {
		prueba.Fatalf("preparar MySQL: %v", err)
	}
	var contador atomic.Int64
	prefijo := fmt.Sprintf("prueba-%d", time.Now().UnixNano())
	servicio, err := Nuevo(repositorioUsuarios, []byte(strings.Repeat("s", 32)),
		func() time.Time { return time.Now().UTC().Truncate(time.Millisecond) },
		func() string { return fmt.Sprintf("%s-%d", prefijo, contador.Add(1)) })
	if err != nil {
		prueba.Fatalf("armar el servicio: %v", err)
	}
	return servicio
}

// emailUnico evita que dos corridas choquen con el índice único.
func emailUnico(nombre string) string {
	return fmt.Sprintf("%s-%d@ejemplo.com", nombre, time.Now().UnixNano())
}

func TestRegistrarseEIniciarSesion(prueba *testing.T) {
	servicio := servicioConMySQL(prueba)
	contexto := context.Background()
	email := emailUnico("ana")

	creado, err := servicio.Registrar(contexto, DatosRegistro{Email: strings.ToUpper(email), Nombre: "Ana", Clave: "clave-segura"})
	if err != nil {
		prueba.Fatalf("registrar: %v", err)
	}
	if creado.Rol != modelo.RolComprador || string(creado.HashClave) == "clave-segura" {
		prueba.Errorf("usuario creado = %+v", creado)
	}

	sesion, err := servicio.IniciarSesion(contexto, email, "clave-segura")
	if err != nil || sesion.Token == "" || sesion.Usuario.ID != creado.ID {
		prueba.Errorf("iniciar sesión = %+v, %v", sesion, err)
	}

	perfil, err := servicio.Perfil(contexto, creado.ID)
	if err != nil || perfil.Email != email {
		prueba.Errorf("perfil = %+v, %v", perfil, err)
	}
}

func TestUnEmailTieneUnaSolaCuenta(prueba *testing.T) {
	servicio := servicioConMySQL(prueba)
	email := emailUnico("beto")
	if _, err := servicio.Registrar(context.Background(), DatosRegistro{Email: email, Nombre: "Beto", Clave: "clave-segura"}); err != nil {
		prueba.Fatalf("registrar: %v", err)
	}

	_, err := servicio.Registrar(context.Background(), DatosRegistro{Email: strings.ToUpper(email), Nombre: "Otro", Clave: "otra-clave"})

	if !errors.Is(err, modelo.ErrEmailRepetido) {
		prueba.Errorf("error = %v, se esperaba ErrEmailRepetido", err)
	}
}

func TestClaveIncorrectaYEmailInexistenteRespondenIgual(prueba *testing.T) {
	servicio := servicioConMySQL(prueba)
	email := emailUnico("caro")
	if _, err := servicio.Registrar(context.Background(), DatosRegistro{Email: email, Nombre: "Caro", Clave: "clave-segura"}); err != nil {
		prueba.Fatalf("registrar: %v", err)
	}

	_, errClave := servicio.IniciarSesion(context.Background(), email, "clave-incorrecta")
	_, errEmail := servicio.IniciarSesion(context.Background(), emailUnico("nadie"), "clave-segura")

	if !errors.Is(errClave, modelo.ErrCredencialesInvalidas) || !errors.Is(errEmail, modelo.ErrCredencialesInvalidas) {
		prueba.Errorf("errores = %v y %v, se esperaba ErrCredencialesInvalidas en los dos", errClave, errEmail)
	}
}

func TestAsegurarVendedorEsIdempotente(prueba *testing.T) {
	servicio := servicioConMySQL(prueba)
	email := emailUnico("vendedor")

	for intento := 1; intento <= 2; intento++ {
		if err := servicio.AsegurarVendedor(context.Background(), email, "clave-del-vendedor"); err != nil {
			prueba.Fatalf("intento %d: %v", intento, err)
		}
	}

	sesion, err := servicio.IniciarSesion(context.Background(), email, "clave-del-vendedor")
	if err != nil || sesion.Usuario.Rol != modelo.RolVendedor {
		prueba.Errorf("sesión del vendedor = %+v, %v", sesion, err)
	}
}

func TestElVendedorNoPuedeUsarElEmailDeUnComprador(prueba *testing.T) {
	servicio := servicioConMySQL(prueba)
	email := emailUnico("dani")
	if _, err := servicio.Registrar(context.Background(), DatosRegistro{Email: email, Nombre: "Dani", Clave: "clave-segura"}); err != nil {
		prueba.Fatalf("registrar: %v", err)
	}

	err := servicio.AsegurarVendedor(context.Background(), strings.ToUpper(email), "clave-del-vendedor")

	if err == nil || !strings.Contains(err.Error(), "VENDEDOR_EMAIL") {
		prueba.Errorf("con el email de un comprador tiene que fallar y nombrar VENDEDOR_EMAIL: %v", err)
	}
}
