package imagenes_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/adaptadores/imagenes"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
)

func TestGuardarYBorrar(prueba *testing.T) {
	directorio := prueba.TempDir()
	disco, err := imagenes.NuevoDisco(directorio, "/imagenes/")
	if err != nil {
		prueba.Fatal(err)
	}

	url, err := disco.Guardar(context.Background(), dominio.Imagen{Contenido: []byte("foto"), Extension: ".png"})
	if err != nil || !strings.HasPrefix(url, "/imagenes/") || !strings.HasSuffix(url, ".png") {
		prueba.Fatalf("url = %q, %v", url, err)
	}
	ruta := filepath.Join(directorio, strings.TrimPrefix(url, "/imagenes/"))
	if contenido, _ := os.ReadFile(ruta); string(contenido) != "foto" {
		prueba.Fatalf("el archivo tiene la imagen: %q", contenido)
	}

	if err := disco.Borrar(context.Background(), url); err != nil {
		prueba.Fatal(err)
	}
	if _, err := os.Stat(ruta); !os.IsNotExist(err) {
		prueba.Error("el archivo se borró")
	}
	if err := disco.Borrar(context.Background(), url); err != nil {
		prueba.Errorf("borrar una imagen que ya no está no es un error: %v", err)
	}
}

func TestBorrarNoSaleDeLaCarpeta(prueba *testing.T) {
	afuera := filepath.Join(prueba.TempDir(), "importante.txt")
	os.WriteFile(afuera, []byte("no borrar"), 0o644)
	disco, _ := imagenes.NuevoDisco(filepath.Join(filepath.Dir(afuera), "imagenes"), "/imagenes/")

	for _, url := range []string{"/imagenes/../importante.txt", "/imagenes/..\\importante.txt", "/otra/importante.txt", "/imagenes/"} {
		if err := disco.Borrar(context.Background(), url); err == nil {
			prueba.Errorf("%q se tenía que rechazar", url)
		}
	}
	if _, err := os.Stat(afuera); err != nil {
		prueba.Errorf("el archivo de afuera sigue: %v", err)
	}
}
