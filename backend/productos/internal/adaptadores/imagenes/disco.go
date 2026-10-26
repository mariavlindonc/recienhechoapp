// Package imagenes guarda las fotos de los productos en una carpeta (un volumen de
// Docker) y arma la URL con la que el api-gateway las sirve (ADR-002).
package imagenes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
)

// Disco cumple puertos.AlmacenDeImagenes.
//
// shortcut: la carpeta es de una sola máquina. Con productos en varias máquinas
// hace falta un almacenamiento compartido (por ejemplo, tipo S3).
type Disco struct {
	directorio string
	prefijoURL string // "/imagenes/"
}

var _ puertos.AlmacenDeImagenes = (*Disco)(nil)

// NuevoDisco usa la carpeta dada, y la crea si no existe.
func NuevoDisco(directorio, prefijoURL string) (*Disco, error) {
	if err := os.MkdirAll(directorio, 0o755); err != nil {
		return nil, fmt.Errorf("crear la carpeta de imágenes: %w", err)
	}
	return &Disco{directorio: directorio, prefijoURL: prefijoURL}, nil
}

// Guardar escribe la imagen con un nombre al azar: cada foto nueva tiene su URL,
// así ningún navegador muestra una vieja guardada en su caché.
func (disco *Disco) Guardar(_ context.Context, imagen dominio.Imagen) (string, error) {
	var azar [16]byte
	rand.Read(azar[:])
	nombre := hex.EncodeToString(azar[:]) + imagen.Extension
	if err := os.WriteFile(filepath.Join(disco.directorio, nombre), imagen.Contenido, 0o644); err != nil {
		return "", fmt.Errorf("escribir la imagen: %w", err)
	}
	return disco.prefijoURL + nombre, nil
}

// Borrar elimina el archivo de esa URL. Solo acepta un nombre de archivo suelto:
// una URL con "../" no puede borrar nada fuera de la carpeta.
func (disco *Disco) Borrar(_ context.Context, url string) error {
	nombre, esNuestra := strings.CutPrefix(url, disco.prefijoURL)
	if !esNuestra || nombre == "" || nombre != filepath.Base(nombre) || strings.ContainsAny(nombre, `/\`) {
		return fmt.Errorf("la URL %q no es de una imagen guardada acá", url)
	}
	err := os.Remove(filepath.Join(disco.directorio, nombre))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Directorio es la carpeta de las imágenes, para servirlas por HTTP.
func (disco *Disco) Directorio() string { return disco.directorio }
