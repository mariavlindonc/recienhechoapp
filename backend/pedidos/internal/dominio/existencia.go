package dominio

// Existencia es el stock propio de un ingrediente, en la unidad de las recetas
// (ADR-020). El stock es el que conocemos del proveedor; lo reservado es lo que
// tienen comprometido los pedidos que todavía no lo consumieron.
type Existencia struct {
	Ingrediente string
	Unidad      string
	Stock       int64
	Reservado   int64
	// ProductoID es el producto del proveedor que usamos como casillero para este
	// ingrediente, o 0 si todavía no tiene ninguno.
	ProductoID int64
}

// Disponible es lo que queda sin comprometer. Es negativo cuando los pedidos
// reservaron más de lo que hay: la reserva nunca rechaza un pedido.
func (existencia Existencia) Disponible() int64 {
	return existencia.Stock - existencia.Reservado
}

// Faltante es cuánto de lo necesario no está cubierto por el stock, contando que
// la reserva del propio pedido ya está sumada en Reservado.
func (existencia Existencia) Faltante(necesario int64) int64 {
	disponible := existencia.Disponible()
	if disponible >= 0 {
		return 0
	}
	return min(-disponible, necesario)
}

// PendienteDeConfirmar es un pedido que espera al vendedor, con los insumos que
// hoy no alcanzan para hacerlo, para que decida con esa información (ADR-020).
type PendienteDeConfirmar struct {
	Pedido    Pedido
	Faltantes []Insumo
}
