package aplicacion_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/aplicacion"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

// Hoy es el 26/10/2026 en Argentina; la medialuna se puede pedir para hoy.
var momento = time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)

const hoy = "2026-10-26"

var comprador = dominio.Comprador{ID: "comprador-1", Email: "cliente@ejemplo.com"}

type entorno struct {
	servicio    *aplicacion.Servicio
	repositorio *repositorioFalso
	inventario  *inventarioFalso
	publicador  *publicadorFalso
	eventos     *eventosFalsos
	reloj       *time.Time
}

func nuevoEntorno() *entorno {
	reloj := momento
	var contador atomic.Int64
	actual := &entorno{
		repositorio: nuevoRepositorioFalso(),
		inventario:  &inventarioFalso{resultado: puertos.ReservaConfirmada},
		publicador:  &publicadorFalso{},
		eventos:     &eventosFalsos{},
		reloj:       &reloj,
	}
	actual.servicio = aplicacion.NuevoServicio(aplicacion.Dependencias{
		Repositorio: actual.repositorio,
		Bandeja:     actual.repositorio,
		Publicador:  actual.publicador,
		Catalogo: catalogoFalso{productos: map[string]dominio.Producto{
			"medialuna": {ID: "medialuna", Nombre: "Medialuna", PrecioCentavos: 80000, AnticipacionDias: 0},
		}},
		Inventario: actual.inventario,
		Eventos:    actual.eventos,
		Ahora:      func() time.Time { return *actual.reloj },
		NuevoID:    func() string { return fmt.Sprintf("id-%d", contador.Add(1)) },
	})
	return actual
}

func (actual *entorno) capacidad(prueba *testing.T, fecha string, capacidad int) {
	prueba.Helper()
	if _, err := actual.servicio.DefinirCapacidad(context.Background(), fecha, capacidad); err != nil {
		prueba.Fatalf("definir capacidad: %v", err)
	}
}

func medialunas(cantidad int, fecha string) dominio.Solicitud {
	return dominio.Solicitud{Tipo: dominio.TipoProgramado, ProductoID: "medialuna", Cantidad: cantidad, Fecha: fecha}
}

func TestCrearReservaElCupoYConfirma(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)

	pedido, err := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(3, hoy))

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if pedido.Estado != dominio.EstadoConfirmado || pedido.TotalCentavos != 240000 {
		prueba.Errorf("estado %s, total %d", pedido.Estado, pedido.TotalCentavos)
	}
	if reservados := actual.repositorio.reservados(hoy); reservados != 3 {
		prueba.Errorf("reservados = %d, se esperaban 3", reservados)
	}
	if estados := actual.repositorio.estadosDeLosEventos(); !slices.Equal(estados, []string{"pendiente_confirmacion", "confirmado"}) {
		prueba.Errorf("eventos = %v", estados)
	}
}

func TestReintentoConLaMismaClaveNoDuplica(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	primero, _ := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(3, hoy))

	segundo, err := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(3, hoy))

	if err != nil || segundo.ID != primero.ID {
		prueba.Fatalf("se esperaba el mismo pedido, dio %s (%v)", segundo.ID, err)
	}
	if reservados := actual.repositorio.reservados(hoy); reservados != 3 {
		prueba.Errorf("el reintento no puede reservar de nuevo: reservados = %d", reservados)
	}

	if _, err := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(4, hoy)); !errors.Is(err, dominio.ErrConflictoIdempotencia) {
		prueba.Errorf("la misma clave con otro pedido tiene que dar conflicto, dio: %v", err)
	}
}

func TestDosCompradoresPorElUltimoCupo(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	actual.servicio.Crear(context.Background(), comprador, "clave-0", medialunas(9, hoy))

	var aceptados, sinCupo atomic.Int32
	var grupo sync.WaitGroup
	for numero := range 2 {
		grupo.Go(func() {
			otro := dominio.Comprador{ID: fmt.Sprintf("otro-%d", numero), Email: "otro@ejemplo.com"}
			_, err := actual.servicio.Crear(context.Background(), otro, "clave", medialunas(1, hoy))
			switch {
			case err == nil:
				aceptados.Add(1)
			case errors.Is(err, dominio.ErrSinCupo):
				sinCupo.Add(1)
			default:
				prueba.Errorf("error inesperado: %v", err)
			}
		})
	}
	grupo.Wait()

	if aceptados.Load() != 1 || sinCupo.Load() != 1 {
		prueba.Errorf("aceptados %d, sin cupo %d: se esperaba uno de cada uno", aceptados.Load(), sinCupo.Load())
	}
}

func TestInmediatoBuscaElPrimerDiaConCupo(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 2)
	actual.capacidad(prueba, "2026-10-27", 0)
	actual.capacidad(prueba, "2026-10-28", 5)

	pedido, err := actual.servicio.Crear(context.Background(), comprador, "clave-1",
		dominio.Solicitud{Tipo: dominio.TipoInmediato, ProductoID: "medialuna", Cantidad: 3})

	if err != nil || pedido.Fecha != "2026-10-28" {
		prueba.Fatalf("fecha = %s (%v), se esperaba 2026-10-28", pedido.Fecha, err)
	}
}

func TestSinCupoNiProductoNoCreanNada(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 1)

	if _, err := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(2, hoy)); !errors.Is(err, dominio.ErrSinCupo) {
		prueba.Errorf("se esperaba ErrSinCupo, dio: %v", err)
	}
	inexistente := medialunas(1, hoy)
	inexistente.ProductoID = "alfajor"
	if _, err := actual.servicio.Crear(context.Background(), comprador, "clave-2", inexistente); !errors.Is(err, dominio.ErrProductoInexistente) {
		prueba.Errorf("se esperaba ErrProductoInexistente, dio: %v", err)
	}
	if len(actual.repositorio.pedidos) != 0 {
		prueba.Errorf("no se tenía que guardar ningún pedido: %d", len(actual.repositorio.pedidos))
	}
}

func TestInventarioCaidoDejaElPedidoPendiente(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	actual.inventario.fallar = errCaido

	pedido, err := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy))

	if err != nil || pedido.Estado != dominio.EstadoPendienteConfirmacion {
		prueba.Fatalf("estado %s (%v): el pedido no se rechaza (R3)", pedido.Estado, err)
	}
	if !pedido.ProximaConfirmacion.Equal(momento.Add(time.Minute)) {
		prueba.Errorf("se reintenta en 1 minuto, quedó para %v", pedido.ProximaConfirmacion)
	}

	// Vuelve el inventario: el proceso en segundo plano lo confirma.
	actual.inventario.fallar = nil
	*actual.reloj = momento.Add(time.Minute)
	if _, err := actual.servicio.ProcesarConfirmaciones(context.Background()); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	confirmado, _ := actual.servicio.Obtener(context.Background(), comprador.ID, pedido.ID)
	if confirmado.Estado != dominio.EstadoConfirmado {
		prueba.Errorf("estado = %s, se esperaba confirmado", confirmado.Estado)
	}
}

func TestSinStockCancelaYLiberaElCupo(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	actual.inventario.resultado = puertos.ReservaSinStock

	pedido, err := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(4, hoy))

	if err != nil || pedido.Estado != dominio.EstadoCancelado {
		prueba.Fatalf("estado %s (%v), se esperaba cancelado", pedido.Estado, err)
	}
	if reservados := actual.repositorio.reservados(hoy); reservados != 0 {
		prueba.Errorf("el cupo se tiene que liberar: reservados = %d", reservados)
	}
}

func TestRechazoDelInventarioSeFrenaParaElVendedor(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	actual.inventario.fallar = fmt.Errorf("%w: 422", puertos.ErrInventarioRechazo)

	pedido, _ := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy))

	if !pedido.RequiereRevision || pedido.Estado != dominio.EstadoPendienteConfirmacion {
		prueba.Fatalf("se esperaba pendiente y marcado para revisión: %s, %v", pedido.Estado, pedido.RequiereRevision)
	}
	*actual.reloj = momento.Add(time.Hour)
	actual.servicio.ProcesarConfirmaciones(context.Background())
	if actual.inventario.reservas != 1 {
		prueba.Errorf("un rechazo no se reintenta: %d reservas", actual.inventario.reservas)
	}
}

func TestCancelarLiberaCupoEInsumos(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	pedido, _ := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(4, hoy))

	cancelado, err := actual.servicio.Cancelar(context.Background(), comprador.ID, pedido.ID)

	if err != nil || cancelado.Estado != dominio.EstadoCancelado {
		prueba.Fatalf("estado %s (%v)", cancelado.Estado, err)
	}
	if reservados := actual.repositorio.reservados(hoy); reservados != 0 {
		prueba.Errorf("reservados = %d, se esperaba 0", reservados)
	}
	if !slices.Equal(actual.inventario.liberados, []string{pedido.ID}) || cancelado.LiberarInsumos {
		prueba.Errorf("los insumos se liberan en el proveedor: %v", actual.inventario.liberados)
	}
}

func TestLiberacionFallidaSeReintenta(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	pedido, _ := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy))
	actual.inventario.fallarLiber = errCaido

	cancelado, _ := actual.servicio.Cancelar(context.Background(), comprador.ID, pedido.ID)
	if !cancelado.LiberarInsumos {
		prueba.Fatal("si el proveedor no respondió, la liberación queda pendiente")
	}

	actual.inventario.fallarLiber = nil
	*actual.reloj = momento.Add(time.Minute)
	actual.servicio.ProcesarConfirmaciones(context.Background())
	if !slices.Equal(actual.inventario.liberados, []string{pedido.ID}) {
		prueba.Errorf("liberados = %v", actual.inventario.liberados)
	}
}

func TestNoSeCancelaEnProduccionNiElPedidoDeOtro(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	pedido, _ := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy))

	if _, err := actual.servicio.Cancelar(context.Background(), "otro", pedido.ID); !errors.Is(err, dominio.ErrNoEncontrado) {
		prueba.Errorf("el pedido de otro comprador no existe para él, dio: %v", err)
	}
	if _, err := actual.servicio.CambiarEstado(context.Background(), pedido.ID, dominio.EstadoEnProduccion); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if _, err := actual.servicio.Cancelar(context.Background(), comprador.ID, pedido.ID); !errors.Is(err, dominio.ErrTransicionInvalida) {
		prueba.Errorf("en producción no se cancela, dio: %v", err)
	}
	if reservados := actual.repositorio.reservados(hoy); reservados != 1 {
		prueba.Errorf("el cupo sigue reservado: %d", reservados)
	}
}

func TestCapacidadNoBajaDeLoReservado(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(6, hoy))

	if _, err := actual.servicio.DefinirCapacidad(context.Background(), hoy, 5); !errors.Is(err, dominio.ErrCapacidadMenorQueReservados) {
		prueba.Errorf("se esperaba ErrCapacidadMenorQueReservados, dio: %v", err)
	}
	if _, err := actual.servicio.DefinirCapacidad(context.Background(), hoy, -1); !errors.Is(err, dominio.ErrSolicitudInvalida) {
		prueba.Errorf("una capacidad negativa es inválida, dio: %v", err)
	}
}

func TestPublicarEventosEnOrdenYEsperarAlBroker(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy))

	actual.publicador.fallar = errCaido
	if publicados, err := actual.servicio.PublicarEventos(context.Background()); publicados != 0 || err != nil {
		prueba.Fatalf("con el broker caído no se publica nada y no es un error: %d, %v", publicados, err)
	}
	if len(actual.repositorio.eventos) != 2 {
		prueba.Fatalf("los eventos esperan en la bandeja: %d", len(actual.repositorio.eventos))
	}

	actual.publicador.fallar = nil
	publicados, err := actual.servicio.PublicarEventos(context.Background())
	if publicados != 2 || err != nil || len(actual.repositorio.eventos) != 0 {
		prueba.Errorf("publicados %d (%v), quedan %d", publicados, err, len(actual.repositorio.eventos))
	}
}

func TestBaseCaidaNoCreaPedido(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	actual.repositorio.fallar = errCaido

	if _, err := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy)); !errors.Is(err, errCaido) {
		prueba.Errorf("se esperaba el error de la base, dio: %v", err)
	}
	if actual.inventario.reservas != 0 {
		prueba.Error("sin pedido guardado no se piden insumos")
	}
}
