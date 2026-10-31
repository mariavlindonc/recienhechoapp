package dominio

import "errors"

// Errores del dominio. El adaptador HTTP los traduce a los códigos del contrato
// (docs/contracts/notificaciones/notificaciones.yaml); acá no se sabe nada de HTTP.
var (
	ErrSolicitudInvalida     = errors.New("solicitud inválida")
	ErrNoEncontrada          = errors.New("notificación inexistente")
	ErrConflictoIdempotencia = errors.New("la clave de idempotencia ya se usó con otro contenido")
	ErrTransicionInvalida    = errors.New("transición de estado inválida")
)
