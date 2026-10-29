package aplicacion

import (
	"context"
	"errors"
	"fmt"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

// SolicitarEspecial guarda la solicitud de un pedido a medida (C6). No reserva
// cupo: lo hace al aceptar la cotización. Repetir con la misma clave devuelve la
// solicitud ya creada.
func (servicio *Servicio) SolicitarEspecial(contexto context.Context, comprador dominio.Comprador, clave, descripcion, fecha string) (dominio.Pedido, error) {
	if err := validarClave(clave); err != nil {
		return dominio.Pedido{}, err
	}
	ahora := servicio.ahora()
	pedido, err := dominio.NuevoEspecial(servicio.nuevoID(), comprador, clave, descripcion, fecha, ahora)
	if err != nil {
		return dominio.Pedido{}, err
	}
	if existente, encontrado, err := servicio.especialYaCreado(contexto, comprador.ID, clave, pedido.Huella); err != nil || encontrado {
		return existente, err
	}
	evento := dominio.NuevoEventoEstadoCambiado(servicio.nuevoID(), pedido, "", ahora)
	err = servicio.repositorio.Registrar(contexto, pedido, evento)
	if errors.Is(err, dominio.ErrPedidoRepetido) {
		existente, _, errExistente := servicio.especialYaCreado(contexto, comprador.ID, clave, pedido.Huella)
		return existente, errExistente
	}
	if err != nil {
		return dominio.Pedido{}, fmt.Errorf("guardar pedido especial: %w", err)
	}
	return pedido, nil
}

func (servicio *Servicio) especialYaCreado(contexto context.Context, idComprador, clave, huella string) (dominio.Pedido, bool, error) {
	existente, err := servicio.repositorio.BuscarPorClave(contexto, idComprador, clave)
	if errors.Is(err, dominio.ErrNoEncontrado) {
		return dominio.Pedido{}, false, nil
	}
	if err != nil {
		return dominio.Pedido{}, false, fmt.Errorf("buscar pedido por clave de idempotencia: %w", err)
	}
	if existente.Huella != huella {
		return dominio.Pedido{}, false, dominio.ErrConflictoIdempotencia
	}
	return existente, true, nil
}

// ListarEspecialesAbiertos es lo que el vendedor tiene para cotizar o esperando respuesta (VE5).
func (servicio *Servicio) ListarEspecialesAbiertos(contexto context.Context) ([]dominio.Pedido, error) {
	pedidos, err := servicio.repositorio.ListarEspecialesAbiertos(contexto)
	if err != nil {
		return nil, fmt.Errorf("listar pedidos especiales: %w", err)
	}
	return pedidos, nil
}

// CotizarEspecial pone el precio (VE5). El evento le avisa al comprador por mail.
func (servicio *Servicio) CotizarEspecial(contexto context.Context, id string, precioCentavos int64) (dominio.Pedido, error) {
	pedido, err := servicio.Obtener(contexto, "", id)
	if err != nil {
		return dominio.Pedido{}, err
	}
	versionLeida := pedido.Version
	ahora := servicio.ahora()
	anterior, err := pedido.Cotizar(precioCentavos, ahora)
	if err != nil {
		return dominio.Pedido{}, err
	}
	evento := dominio.NuevoEventoEstadoCambiado(servicio.nuevoID(), pedido, anterior, ahora)
	if err := servicio.guardar(contexto, &pedido, puertos.Cambio{Pedido: pedido, VersionAnterior: versionLeida, Evento: &evento}); err != nil {
		return dominio.Pedido{}, err
	}
	return pedido, nil
}

// ResponderEspecial acepta o rechaza la cotización (C6). Al aceptar reserva el
// cupo del día y sigue como un pedido pendiente de confirmación. Una cotización de
// más de 48 h queda vencida y responde ErrCotizacionVencida (R8).
func (servicio *Servicio) ResponderEspecial(contexto context.Context, idComprador, id string, acepta bool) (dominio.Pedido, error) {
	pedido, err := servicio.Obtener(contexto, idComprador, id)
	if err != nil {
		return dominio.Pedido{}, err
	}
	versionLeida := pedido.Version
	ahora := servicio.ahora()
	anterior, errRespuesta := pedido.ResponderCotizacion(acepta, ahora)
	if errRespuesta != nil && !errors.Is(errRespuesta, dominio.ErrCotizacionVencida) {
		return dominio.Pedido{}, errRespuesta
	}
	evento := dominio.NuevoEventoEstadoCambiado(servicio.nuevoID(), pedido, anterior, ahora)
	cambio := puertos.Cambio{Pedido: pedido, VersionAnterior: versionLeida, Evento: &evento, ReservarCupo: pedido.Estado == dominio.EstadoPendienteConfirmacion}
	if err := servicio.guardar(contexto, &pedido, cambio); err != nil {
		return dominio.Pedido{}, err
	}
	if errRespuesta != nil {
		return pedido, errRespuesta
	}
	if pedido.Estado == dominio.EstadoPendienteConfirmacion {
		// Igual que al crear un pedido: se intenta confirmar ya, y si no termina lo
		// retoma el proceso en segundo plano.
		contextoConfirmacion, cancelar := context.WithTimeout(context.WithoutCancel(contexto), esperaConfirmacionInicial)
		defer cancelar()
		if err := servicio.confirmar(contextoConfirmacion, &pedido); errors.Is(err, dominio.ErrModificadoAlMismoTiempo) {
			return servicio.repositorio.Buscar(contexto, pedido.ID)
		}
	}
	return pedido, nil
}
