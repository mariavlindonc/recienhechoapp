// Package aplicacion tiene los casos de uso de pedidos: encargar (con
// idempotencia), cancelar, avanzar estados, definir la capacidad de cada día y
// los procesos en segundo plano (reconfirmar con el inventario, liberar insumos y
// publicar eventos). Usa el dominio para las reglas y los puertos para todo lo demás.
package aplicacion

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

const (
	largoMaximoClave = 100

	// diasDeBusqueda es hasta cuántos días después de la primera fecha posible se
	// busca cupo para un pedido inmediato.
	diasDeBusqueda = 30

	// tiempoApartado es cuánto queda reservado un pedido o un evento para la
	// instancia que lo tomó. Si se cae a mitad de camino, pasado este tiempo otra
	// lo vuelve a tomar.
	tiempoApartado = 2 * time.Minute

	// tiempoApartadoEvento es lo mismo para un evento de la bandeja de salida. Es
	// más corto porque publicar corta a los 5 s: si el broker estaba caído, el
	// evento se vuelve a intentar enseguida que vuelve.
	tiempoApartadoEvento = 15 * time.Second

	// esperaConfirmacionInicial es cuánto puede tardar la confirmación que se hace
	// al crear el pedido: el inventario tiene 1,5 s (ADR-010) más el guardado.
	esperaConfirmacionInicial = 5 * time.Second

	// esperaReintentoLiberar es cada cuánto se reintenta avisarle al proveedor que
	// libere insumos.
	esperaReintentoLiberar = time.Minute
)

// Servicio reúne los casos de uso.
type Servicio struct {
	repositorio puertos.Repositorio
	bandeja     puertos.BandejaDeSalida
	publicador  puertos.Publicador
	catalogo    puertos.Catalogo
	inventario  puertos.ReservadorDeInsumos
	eventos     puertos.Eventos
	ahora       func() time.Time
	nuevoID     func() string
}

// Dependencias del servicio. El reloj y el generador de ids se reciben de afuera
// para que los tests puedan controlarlos.
type Dependencias struct {
	Repositorio puertos.Repositorio
	Bandeja     puertos.BandejaDeSalida
	Publicador  puertos.Publicador
	Catalogo    puertos.Catalogo
	Inventario  puertos.ReservadorDeInsumos
	Eventos     puertos.Eventos
	Ahora       func() time.Time
	NuevoID     func() string
}

// NuevoServicio arma el servicio.
func NuevoServicio(dependencias Dependencias) *Servicio {
	return &Servicio{
		repositorio: dependencias.Repositorio,
		bandeja:     dependencias.Bandeja,
		publicador:  dependencias.Publicador,
		catalogo:    dependencias.Catalogo,
		inventario:  dependencias.Inventario,
		eventos:     dependencias.Eventos,
		ahora:       dependencias.Ahora,
		nuevoID:     dependencias.NuevoID,
	}
}

// Crear encarga un pedido: fija el precio, reserva el cupo del día y después pide
// los insumos al inventario. Repetir el pedido con la misma clave devuelve el ya
// creado, sin reservar el cupo otra vez (R1).
func (servicio *Servicio) Crear(contexto context.Context, comprador dominio.Comprador, clave string, solicitud dominio.Solicitud) (dominio.Pedido, error) {
	if err := validarClave(clave); err != nil {
		return dominio.Pedido{}, err
	}
	if err := solicitud.Validar(); err != nil {
		return dominio.Pedido{}, err
	}
	if existente, encontrado, err := servicio.pedidoYaCreado(contexto, comprador.ID, clave, solicitud); err != nil || encontrado {
		return existente, err
	}

	producto, err := servicio.catalogo.ObtenerProducto(contexto, solicitud.ProductoID)
	if err != nil {
		return dominio.Pedido{}, fmt.Errorf("obtener producto %s: %w", solicitud.ProductoID, err)
	}

	pedido, err := servicio.registrar(contexto, comprador, clave, solicitud, producto)
	if errors.Is(err, dominio.ErrPedidoRepetido) {
		// Otro intento con la misma clave ganó la carrera: se devuelve ese.
		existente, _, errExistente := servicio.pedidoYaCreado(contexto, comprador.ID, clave, solicitud)
		return existente, errExistente
	}
	if err != nil {
		return dominio.Pedido{}, err
	}

	// El pedido ya está guardado con su cupo. La confirmación sigue aunque el
	// comprador corte la conexión; si no termina, la retoma el proceso en segundo plano.
	contextoConfirmacion, cancelar := context.WithTimeout(context.WithoutCancel(contexto), esperaConfirmacionInicial)
	defer cancelar()
	if err := servicio.confirmar(contextoConfirmacion, &pedido); errors.Is(err, dominio.ErrModificadoAlMismoTiempo) {
		// Alguien lo cambió en el medio (por ejemplo, el comprador lo canceló).
		return servicio.repositorio.Buscar(contexto, pedido.ID)
	}
	// Si la confirmación falló por otra cosa, el pedido igual existe y queda
	// pendiente_confirmacion hasta que lo retome el proceso en segundo plano.
	return pedido, nil
}

// pedidoYaCreado busca el pedido de un intento anterior con la misma clave.
func (servicio *Servicio) pedidoYaCreado(contexto context.Context, idComprador, clave string, solicitud dominio.Solicitud) (dominio.Pedido, bool, error) {
	existente, err := servicio.repositorio.BuscarPorClave(contexto, idComprador, clave)
	if errors.Is(err, dominio.ErrNoEncontrado) {
		return dominio.Pedido{}, false, nil
	}
	if err != nil {
		return dominio.Pedido{}, false, fmt.Errorf("buscar pedido por clave de idempotencia: %w", err)
	}
	if existente.Huella != solicitud.Huella() {
		return dominio.Pedido{}, false, dominio.ErrConflictoIdempotencia
	}
	return existente, true, nil
}

// registrar guarda el pedido con su cupo. Un programado va a su fecha; un
// inmediato prueba desde la primera fecha posible hasta encontrar un día con cupo.
func (servicio *Servicio) registrar(contexto context.Context, comprador dominio.Comprador, clave string, solicitud dominio.Solicitud, producto dominio.Producto) (dominio.Pedido, error) {
	ahora := servicio.ahora()
	fechas := []string{solicitud.Fecha}
	if solicitud.Tipo == dominio.TipoInmediato {
		primera := dominio.PrimeraFechaPosible(dominio.Hoy(ahora), producto.AnticipacionDias)
		fechas = make([]string, diasDeBusqueda)
		for dia := range fechas {
			fechas[dia] = dominio.SumarDias(primera, dia)
		}
	}

	id := servicio.nuevoID()
	for _, fecha := range fechas {
		pedido, err := dominio.NuevoPedido(id, comprador, clave, solicitud, producto, fecha, ahora)
		if err != nil {
			return dominio.Pedido{}, err
		}
		// Hasta que termine la confirmación de este pedido, el proceso en segundo
		// plano no lo toma.
		pedido.ProximaConfirmacion = ahora.Add(tiempoApartado)
		evento := dominio.NuevoEventoEstadoCambiado(servicio.nuevoID(), pedido, "", ahora)

		err = servicio.repositorio.Registrar(contexto, pedido, evento)
		if errors.Is(err, dominio.ErrSinCupo) {
			continue
		}
		if err != nil {
			if !errors.Is(err, dominio.ErrPedidoRepetido) {
				err = fmt.Errorf("registrar pedido: %w", err)
			}
			return dominio.Pedido{}, err
		}
		servicio.eventos.PedidoRegistrado(contexto, "aceptado")
		return pedido, nil
	}
	servicio.eventos.PedidoRegistrado(contexto, "sin_cupo")
	return dominio.Pedido{}, dominio.ErrSinCupo
}

// confirmar pide los insumos al inventario y guarda el resultado (ADR-010).
func (servicio *Servicio) confirmar(contexto context.Context, pedido *dominio.Pedido) error {
	resultado, errReserva := servicio.inventario.Reservar(contexto, pedido.ID, pedido.Insumos)
	ahora := servicio.ahora()
	versionLeida := pedido.Version

	switch {
	case errReserva == nil && resultado == puertos.ReservaConfirmada:
		servicio.eventos.InventarioConsultado(contexto, pedido.ID, puertos.InventarioConfirmada, nil)
		return servicio.cambiarEstado(contexto, pedido, dominio.EstadoConfirmado, "", ahora)
	case errReserva == nil:
		servicio.eventos.InventarioConsultado(contexto, pedido.ID, puertos.InventarioSinStock, nil)
		return servicio.cambiarEstado(contexto, pedido, dominio.EstadoCancelado, "No hay insumos para esa fecha.", ahora)
	case errors.Is(errReserva, puertos.ErrInventarioRechazo):
		// Reintentar no arregla un error de formato: se frena y lo decide el vendedor.
		servicio.eventos.InventarioConsultado(contexto, pedido.ID, puertos.InventarioRechazada, errReserva)
		pedido.MarcarParaRevision("El inventario rechazó la reserva de insumos.", ahora)
	case errors.Is(errReserva, puertos.ErrInventarioNoDisponible):
		servicio.eventos.InventarioConsultado(contexto, pedido.ID, puertos.InventarioNoDisponible, errReserva)
		pedido.RegistrarFalloDeConfirmacion(ahora)
	default:
		servicio.eventos.InventarioConsultado(contexto, pedido.ID, puertos.InventarioFallaTransit, errReserva)
		pedido.RegistrarFalloDeConfirmacion(ahora)
	}
	return servicio.guardar(contexto, pedido, puertos.Cambio{Pedido: *pedido, VersionAnterior: versionLeida})
}

// cambiarEstado aplica la transición y la guarda junto con su evento y, si se
// cancela, con la liberación del cupo.
func (servicio *Servicio) cambiarEstado(contexto context.Context, pedido *dominio.Pedido, nuevo dominio.Estado, motivo string, ahora time.Time) error {
	versionLeida := pedido.Version
	anterior, err := pedido.CambiarEstado(nuevo, motivo, ahora)
	if err != nil {
		return err
	}
	evento := dominio.NuevoEventoEstadoCambiado(servicio.nuevoID(), *pedido, anterior, ahora)
	return servicio.guardar(contexto, pedido, puertos.Cambio{
		Pedido:          *pedido,
		VersionAnterior: versionLeida,
		Evento:          &evento,
		LiberarCupo:     nuevo == dominio.EstadoCancelado,
	})
}

func (servicio *Servicio) guardar(contexto context.Context, pedido *dominio.Pedido, cambio puertos.Cambio) error {
	if err := servicio.repositorio.GuardarCambio(contexto, cambio); err != nil {
		if errors.Is(err, dominio.ErrModificadoAlMismoTiempo) {
			return err
		}
		return fmt.Errorf("guardar pedido %s: %w", pedido.ID, err)
	}
	pedido.Version = cambio.VersionAnterior + 1
	return nil
}

// Obtener devuelve un pedido. Un comprador solo ve los suyos; para él, los de
// otro no existen.
func (servicio *Servicio) Obtener(contexto context.Context, idComprador, id string) (dominio.Pedido, error) {
	pedido, err := servicio.repositorio.Buscar(contexto, id)
	if err != nil {
		return dominio.Pedido{}, fmt.Errorf("buscar pedido %s: %w", id, err)
	}
	if idComprador != "" && !pedido.PerteneceA(idComprador) {
		return dominio.Pedido{}, dominio.ErrNoEncontrado
	}
	return pedido, nil
}

// ListarDeComprador devuelve los pedidos del comprador (C7).
func (servicio *Servicio) ListarDeComprador(contexto context.Context, idComprador string) ([]dominio.Pedido, error) {
	pedidos, err := servicio.repositorio.ListarDeComprador(contexto, idComprador)
	if err != nil {
		return nil, fmt.Errorf("listar pedidos del comprador: %w", err)
	}
	return pedidos, nil
}

// ListarDelDia devuelve lo que hay que producir en una fecha (VE4).
func (servicio *Servicio) ListarDelDia(contexto context.Context, fecha string) ([]dominio.Pedido, error) {
	if err := dominio.ValidarFecha(fecha); err != nil {
		return nil, err
	}
	pedidos, err := servicio.repositorio.ListarDelDia(contexto, fecha)
	if err != nil {
		return nil, fmt.Errorf("listar pedidos del día: %w", err)
	}
	return pedidos, nil
}

// Cancelar cancela un pedido del comprador antes de que entre en producción (C8,
// R6) y libera su cupo y sus insumos (R7).
func (servicio *Servicio) Cancelar(contexto context.Context, idComprador, id string) (dominio.Pedido, error) {
	pedido, err := servicio.Obtener(contexto, idComprador, id)
	if err != nil {
		return dominio.Pedido{}, err
	}
	return servicio.aplicarEstado(contexto, pedido, dominio.EstadoCancelado, "Cancelado por el comprador.")
}

// CambiarEstado lo usa el vendedor para avanzar un pedido (VE6) o cancelarlo.
func (servicio *Servicio) CambiarEstado(contexto context.Context, id string, nuevo dominio.Estado) (dominio.Pedido, error) {
	pedido, err := servicio.Obtener(contexto, "", id)
	if err != nil {
		return dominio.Pedido{}, err
	}
	return servicio.aplicarEstado(contexto, pedido, nuevo, "Cancelado por la pastelería.")
}

func (servicio *Servicio) aplicarEstado(contexto context.Context, pedido dominio.Pedido, nuevo dominio.Estado, motivo string) (dominio.Pedido, error) {
	if err := servicio.cambiarEstado(contexto, &pedido, nuevo, motivo, servicio.ahora()); err != nil {
		return dominio.Pedido{}, err
	}
	if pedido.LiberarInsumos {
		// Se intenta enseguida; si el proveedor no responde, lo reintenta el proceso
		// en segundo plano.
		servicio.liberar(contexto, &pedido)
	}
	return pedido, nil
}

// DefinirCapacidad fija cuántas unidades se pueden producir un día (VE3).
func (servicio *Servicio) DefinirCapacidad(contexto context.Context, fecha string, capacidad int) (dominio.Dia, error) {
	if err := dominio.ValidarFecha(fecha); err != nil {
		return dominio.Dia{}, err
	}
	if capacidad < 0 {
		return dominio.Dia{}, fmt.Errorf("%w: la capacidad no puede ser negativa", dominio.ErrSolicitudInvalida)
	}
	dia, err := servicio.repositorio.DefinirCapacidad(contexto, fecha, capacidad)
	if err != nil && !errors.Is(err, dominio.ErrCapacidadMenorQueReservados) {
		return dominio.Dia{}, fmt.Errorf("definir capacidad del %s: %w", fecha, err)
	}
	return dia, err
}

// ListarCapacidad devuelve la capacidad y lo reservado de cada día del rango.
func (servicio *Servicio) ListarCapacidad(contexto context.Context, desde, hasta string) ([]dominio.Dia, error) {
	if err := errors.Join(dominio.ValidarFecha(desde), dominio.ValidarFecha(hasta)); err != nil {
		return nil, err
	}
	dias, err := servicio.repositorio.ListarCapacidad(contexto, desde, hasta)
	if err != nil {
		return nil, fmt.Errorf("listar capacidad: %w", err)
	}
	return dias, nil
}

// ProcesarConfirmaciones reintenta la reserva de insumos de los pedidos pendientes
// y la liberación de los cancelados. Lo llama periódicamente un proceso en
// segundo plano y devuelve cuántos pedidos procesó.
func (servicio *Servicio) ProcesarConfirmaciones(contexto context.Context) (int, error) {
	procesados := 0
	for {
		if err := contexto.Err(); err != nil {
			return procesados, err
		}
		ahora := servicio.ahora()
		pedido, encontrado, err := servicio.repositorio.TomarParaConfirmar(contexto, ahora, ahora.Add(tiempoApartado))
		if err != nil {
			return procesados, fmt.Errorf("tomar pedido para confirmar: %w", err)
		}
		if !encontrado {
			break
		}
		if err := servicio.confirmar(contexto, &pedido); err != nil && !errors.Is(err, dominio.ErrModificadoAlMismoTiempo) {
			return procesados, err
		}
		procesados++
	}
	for {
		if err := contexto.Err(); err != nil {
			return procesados, err
		}
		ahora := servicio.ahora()
		pedido, encontrado, err := servicio.repositorio.TomarParaLiberar(contexto, ahora, ahora.Add(tiempoApartado))
		if err != nil {
			return procesados, fmt.Errorf("tomar pedido para liberar insumos: %w", err)
		}
		if !encontrado {
			return procesados, nil
		}
		servicio.liberar(contexto, &pedido)
		procesados++
	}
}

// liberar le pide al proveedor que libere los insumos del pedido. Si no puede,
// lo deja para el próximo reintento.
func (servicio *Servicio) liberar(contexto context.Context, pedido *dominio.Pedido) {
	errLiberar := servicio.inventario.Liberar(contexto, pedido.ID)
	ahora := servicio.ahora()
	versionLeida := pedido.Version
	if errLiberar != nil {
		servicio.eventos.InventarioConsultado(contexto, pedido.ID, puertos.InventarioFallaTransit, errLiberar)
		pedido.ProximaLiberacion = ahora.Add(esperaReintentoLiberar)
	} else {
		servicio.eventos.InventarioConsultado(contexto, pedido.ID, puertos.InventarioLiberada, nil)
		pedido.InsumosLiberados(ahora)
	}
	// Si falla el guardado, el pedido sigue con LiberarInsumos y se vuelve a
	// intentar: Liberar es idempotente del lado del proveedor.
	_ = servicio.guardar(contexto, pedido, puertos.Cambio{Pedido: *pedido, VersionAnterior: versionLeida})
}

// PublicarEventos manda al broker los eventos de la bandeja de salida, del más
// viejo al más nuevo, y devuelve cuántos publicó. Si el broker no responde, corta:
// los eventos esperan guardados hasta que vuelva (ADR-011).
func (servicio *Servicio) PublicarEventos(contexto context.Context) (int, error) {
	publicados := 0
	for {
		if err := contexto.Err(); err != nil {
			return publicados, err
		}
		ahora := servicio.ahora()
		guardado, encontrado, err := servicio.bandeja.TomarEvento(contexto, ahora, ahora.Add(tiempoApartadoEvento))
		if err != nil {
			return publicados, fmt.Errorf("tomar evento de la bandeja de salida: %w", err)
		}
		if !encontrado {
			return publicados, nil
		}
		if err := servicio.publicador.Publicar(contexto, guardado); err != nil {
			servicio.eventos.EventoPublicado(contexto, guardado.Evento.ID, err)
			return publicados, nil
		}
		// Si esto falla, el evento se vuelve a publicar cuando vence el apartado: el
		// consumidor descarta los repetidos por idEvento (entrega "al menos una vez").
		if err := servicio.bandeja.MarcarPublicado(contexto, guardado.Evento.ID); err != nil {
			return publicados, fmt.Errorf("marcar evento %s como publicado: %w", guardado.Evento.ID, err)
		}
		servicio.eventos.EventoPublicado(contexto, guardado.Evento.ID, nil)
		publicados++
	}
}

func validarClave(clave string) error {
	largo := utf8.RuneCountInString(clave)
	if largo == 0 || largo > largoMaximoClave {
		return fmt.Errorf("%w: la Idempotency-Key tiene que tener entre 1 y %d caracteres", dominio.ErrSolicitudInvalida, largoMaximoClave)
	}
	return nil
}
