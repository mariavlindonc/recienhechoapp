package puertos

import "time"

// OperacionDeIndice es lo que hay que hacer con un producto en el índice de búsqueda.
type OperacionDeIndice string

const (
	IndiceGuardar OperacionDeIndice = "guardar"
	IndiceBorrar  OperacionDeIndice = "borrar"
)

// CambioDeIndice es una fila de la bandeja: un producto que cambió.
type CambioDeIndice struct {
	ID         int64
	ProductoID string
	Operacion  OperacionDeIndice
	CreadoEn   time.Time
}

// CambiosDeIndice son filas de la bandeja, de la más vieja a la más nueva.
type CambiosDeIndice []CambioDeIndice

// Resolver decide qué hacer con cada producto: vale la última operación, porque un
// producto que se modificó y después se dio de baja se borra, y uno con varias
// modificaciones se guarda una sola vez.
func (cambios CambiosDeIndice) Resolver() (guardar, borrar []string) {
	ultima := make(map[string]OperacionDeIndice, len(cambios))
	var orden []string
	for _, cambio := range cambios {
		if _, visto := ultima[cambio.ProductoID]; !visto {
			orden = append(orden, cambio.ProductoID)
		}
		ultima[cambio.ProductoID] = cambio.Operacion
	}
	for _, productoID := range orden {
		if ultima[productoID] == IndiceBorrar {
			borrar = append(borrar, productoID)
		} else {
			guardar = append(guardar, productoID)
		}
	}
	return guardar, borrar
}

// IDs son los números de fila, para quitarlas de la bandeja una vez procesadas.
func (cambios CambiosDeIndice) IDs() []int64 {
	ids := make([]int64, len(cambios))
	for posicion, cambio := range cambios {
		ids[posicion] = cambio.ID
	}
	return ids
}

// Antiguedad es hace cuánto espera el cambio más viejo (RNF1: menos de 10 s). Es
// 0 si no hay cambios.
func (cambios CambiosDeIndice) Antiguedad(ahora time.Time) time.Duration {
	if len(cambios) == 0 {
		return 0
	}
	return ahora.Sub(cambios[0].CreadoEn)
}
