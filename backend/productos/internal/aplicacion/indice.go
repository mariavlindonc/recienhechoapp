package aplicacion

import (
	"context"
	"fmt"
	"slices"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
)

const (
	// loteDelIndice es cuántas filas de la bandeja se procesan por vuelta.
	loteDelIndice = 100
	// loteDeReindexado es cuántos productos se cargan por vez al reconstruir el índice.
	loteDeReindexado = 500
)

// SincronizarIndice refleja en la búsqueda los productos que cambiaron (ADR-008).
// Corre en segundo plano cada 2 s. Si el motor no responde, las filas quedan en la
// bandeja y se reintentan en la vuelta siguiente: no se pierde ningún cambio.
//
// shortcut: con dos instancias, las dos pueden procesar las mismas filas. No hace
// daño, porque guardar y borrar en el índice se pueden repetir; se aparta cada
// fila por instancia si la carga lo pide.
func (servicio *Servicio) SincronizarIndice(contexto context.Context) (int, error) {
	cambios, err := servicio.bandeja.CambiosPendientes(contexto, loteDelIndice)
	if err != nil {
		err = fmt.Errorf("leer la bandeja del índice: %w", err)
		servicio.eventos.IndiceSincronizado(contexto, 0, err)
		return 0, err
	}
	servicio.eventos.AntiguedadDelIndice(contexto, cambios.Antiguedad(servicio.ahora()))
	if len(cambios) == 0 {
		return 0, nil
	}
	cantidad, err := servicio.reflejar(contexto, cambios)
	servicio.eventos.IndiceSincronizado(contexto, cantidad, err)
	return cantidad, err
}

func (servicio *Servicio) reflejar(contexto context.Context, cambios puertos.CambiosDeIndice) (int, error) {
	guardar, borrar := cambios.Resolver()
	guardados := 0
	if len(guardar) > 0 {
		productos, err := servicio.bandeja.ProductosParaIndice(contexto, guardar)
		if err != nil {
			return 0, fmt.Errorf("leer los productos a indexar: %w", err)
		}
		// Los que no volvieron se dieron de baja después de encolarse.
		borrar = append(borrar, faltantes(guardar, productos)...)
		if len(productos) > 0 {
			if err := servicio.buscador.Guardar(contexto, productos); err != nil {
				return 0, fmt.Errorf("guardar en el índice: %w", err)
			}
		}
		guardados = len(productos)
	}
	if len(borrar) > 0 {
		if err := servicio.buscador.Borrar(contexto, borrar); err != nil {
			return 0, fmt.Errorf("borrar del índice: %w", err)
		}
	}
	if err := servicio.bandeja.QuitarCambios(contexto, cambios.IDs()); err != nil {
		// El índice ya está bien; las filas se vuelven a procesar sin hacer daño.
		return 0, fmt.Errorf("quitar las filas de la bandeja: %w", err)
	}
	return guardados + len(borrar), nil
}

// faltantes son los ids pedidos que no están entre los productos encontrados.
func faltantes(ids []string, encontrados []dominio.Producto) []string {
	var resultado []string
	for _, id := range ids {
		if !slices.ContainsFunc(encontrados, func(producto dominio.Producto) bool { return producto.ID == id }) {
			resultado = append(resultado, id)
		}
	}
	return resultado
}

// Reindexar reconstruye el índice entero desde la base, sin dejar la búsqueda vacía
// mientras tanto (ADR-008). Lo corre el comando `productos reindexar`. Devuelve
// cuántos productos quedaron indexados.
//
// shortcut: un producto que cambia mientras corre puede quedar con la versión
// leída al principio hasta su próximo cambio. Se corre en un momento sin
// actividad del vendedor.
func (servicio *Servicio) Reindexar(contexto context.Context) (int, error) {
	reconstruccion, err := servicio.buscador.IniciarReconstruccion(contexto)
	if err != nil {
		return 0, fmt.Errorf("iniciar la reconstrucción del índice: %w", err)
	}
	total, err := servicio.cargar(contexto, reconstruccion)
	if err == nil {
		err = reconstruccion.Intercambiar(contexto)
	}
	if err != nil {
		reconstruccion.Descartar(contexto)
		return 0, fmt.Errorf("reconstruir el índice: %w", err)
	}
	return total, nil
}

func (servicio *Servicio) cargar(contexto context.Context, reconstruccion puertos.Reconstruccion) (int, error) {
	total, despuesDe := 0, ""
	for {
		lote, err := servicio.bandeja.ListarParaReindexar(contexto, despuesDe, loteDeReindexado)
		if err != nil {
			return 0, err
		}
		if len(lote) == 0 {
			return total, nil
		}
		if err := reconstruccion.Agregar(contexto, lote); err != nil {
			return 0, err
		}
		total += len(lote)
		despuesDe = lote[len(lote)-1].ID
	}
}
