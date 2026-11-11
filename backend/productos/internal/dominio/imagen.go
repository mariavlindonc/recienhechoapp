package dominio

import (
	"bytes"
	"fmt"
)

// MaximoBytesImagen es el tamaño máximo de una imagen de producto: 2 MB.
const MaximoBytesImagen = 2 << 20

// Imagen es el archivo de la foto de un producto, ya validado.
type Imagen struct {
	Contenido []byte
	Extension string // con el punto: ".png"
}

// NuevaImagen decide si el archivo es una imagen aceptada mirando su contenido, no
// el nombre ni el tipo que dice el navegador: un archivo .png que en realidad es
// otra cosa se rechaza.
func NuevaImagen(contenido []byte) (Imagen, error) {
	if len(contenido) == 0 || len(contenido) > MaximoBytesImagen {
		return Imagen{}, fmt.Errorf("%w: tiene que pesar hasta %d MB", ErrImagenInvalida, MaximoBytesImagen>>20)
	}
	switch {
	case bytes.HasPrefix(contenido, []byte("\x89PNG\r\n\x1a\n")):
		return Imagen{Contenido: contenido, Extension: ".png"}, nil
	case bytes.HasPrefix(contenido, []byte{0xFF, 0xD8, 0xFF}):
		return Imagen{Contenido: contenido, Extension: ".jpg"}, nil
	case len(contenido) >= 12 && bytes.Equal(contenido[0:4], []byte("RIFF")) && bytes.Equal(contenido[8:12], []byte("WEBP")):
		return Imagen{Contenido: contenido, Extension: ".webp"}, nil
	}
	return Imagen{}, fmt.Errorf("%w: tiene que ser JPG, PNG o WebP", ErrImagenInvalida)
}
