package dominio

import "errors"

// Errores del dominio. El adaptador HTTP los traduce a códigos de respuesta; acá
// no se sabe nada de HTTP.
var (
	ErrSolicitudInvalida           = errors.New("solicitud inválida")
	ErrNoEncontrado                = errors.New("pedido inexistente")
	ErrProductoInexistente         = errors.New("el producto no existe o no está disponible")
	ErrAnticipacionInsuficiente    = errors.New("el pedido no cumple la anticipación mínima del producto")
	ErrSinCupo                     = errors.New("sin cupo para ese día")
	ErrTransicionInvalida          = errors.New("transición de estado inválida")
	ErrConflictoIdempotencia       = errors.New("la clave de idempotencia ya se usó con otro pedido")
	ErrPedidoRepetido              = errors.New("ya existe un pedido con esa clave de idempotencia")
	ErrModificadoAlMismoTiempo     = errors.New("el pedido cambió mientras se lo modificaba")
	ErrCapacidadMenorQueReservados = errors.New("la capacidad no puede ser menor a lo ya reservado")
	ErrCotizacionVencida           = errors.New("la cotización venció")
)
