package dominio

import "errors"

// Errores de las reglas de negocio. El adaptador HTTP los traduce a códigos de
// respuesta; acá no se sabe nada de HTTP. Los errores técnicos (un motor que no
// responde) están en puertos.
var (
	ErrSolicitudInvalida     = errors.New("solicitud inválida")
	ErrNoEncontrado          = errors.New("producto inexistente")
	ErrCategoriaNoEncontrada = errors.New("categoría inexistente")
	ErrCategoriaRepetida     = errors.New("ya existe una categoría con ese nombre")
	ErrCategoriaEnUso        = errors.New("la categoría tiene productos")
	ErrImagenInvalida        = errors.New("imagen inválida")
)
