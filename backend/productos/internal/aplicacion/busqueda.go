package aplicacion

import (
	"context"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/dominio"
)

// BuscarProductos busca con texto, filtros y orden (C3). No se cachea: la resuelve
// el motor de búsqueda (ADR-009). Si el motor no responde, devuelve
// puertos.ErrBusquedaNoDisponible y el comprador sigue con el catálogo.
func (servicio *Servicio) BuscarProductos(contexto context.Context, pedida dominio.Consulta) (dominio.PaginaDeProductos, error) {
	consulta, err := dominio.NuevaConsulta(pedida)
	if err != nil {
		return dominio.PaginaDeProductos{}, err
	}
	return servicio.buscador.Buscar(contexto, consulta)
}
