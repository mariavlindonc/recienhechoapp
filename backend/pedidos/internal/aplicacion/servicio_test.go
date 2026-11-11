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
	existencias *existenciasFalsas
	proveedor   *proveedorFalso
	publicador  *publicadorFalso
	eventos     *eventosFalsos
	avisos      *avisosFalsos
	reloj       *time.Time
}

// nuevoEntorno reparte los productos 1, 2 y 3 del proveedor entre los ingredientes,
// en el orden en que aparecen.
func nuevoEntorno() *entorno {
	return nuevoEntornoConProductos(1, 2, 3)
}

func nuevoEntornoConProductos(productos ...int64) *entorno {
	reloj := momento
	var contador atomic.Int64
	existencias := nuevasExistenciasFalsas(productos...)
	actual := &entorno{
		repositorio: nuevoRepositorioFalso(existencias),
		existencias: existencias,
		proveedor:   nuevoProveedorFalso(),
		publicador:  &publicadorFalso{},
		eventos:     &eventosFalsos{},
		avisos:      &avisosFalsos{},
		reloj:       &reloj,
	}
	actual.servicio = aplicacion.NuevoServicio(aplicacion.Dependencias{
		Repositorio: actual.repositorio,
		Bandeja:     actual.repositorio,
		Publicador:  actual.publicador,
		Catalogo: catalogoFalso{productos: map[string]dominio.Producto{
			"medialuna": {
				ID: "medialuna", Nombre: "Medialuna", PrecioCentavos: 80000, AnticipacionDias: 0,
				Receta: []dominio.Insumo{{Ingrediente: "harina", Cantidad: 50, Unidad: "g"}, {Ingrediente: "manteca", Cantidad: 20, Unidad: "g"}},
			},
			"torta": {ID: "torta", Nombre: "Torta", PrecioCentavos: 1250000, AnticipacionDias: 2},
		}},
		Existencias: actual.existencias,
		Inventario:  actual.proveedor,
		Eventos:     actual.eventos,
		Avisos:      actual.avisos,
		Ahora:       func() time.Time { return *actual.reloj },
		NuevoID:     func() string { return fmt.Sprintf("id-%d", contador.Add(1)) },
	})
	return actual
}

func (actual *entorno) capacidad(prueba *testing.T, fecha string, capacidad int) {
	prueba.Helper()
	if _, err := actual.servicio.DefinirCapacidad(context.Background(), fecha, capacidad); err != nil {
		prueba.Fatalf("definir capacidad: %v", err)
	}
}

// pedidoEnProduccion encarga una medialuna, la confirma el vendedor y la pasa a
// producción, con stock de sobra en el proveedor y en el propio.
func (actual *entorno) pedidoEnProduccion(prueba *testing.T) dominio.Pedido {
	prueba.Helper()
	actual.capacidad(prueba, hoy, 10)
	actual.existencias.conStock("harina", "g", 1000)
	actual.existencias.conStock("manteca", "g", 1000)
	actual.proveedor.stocks[1], actual.proveedor.stocks[2] = 1000, 1000
	pedido, err := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy))
	if err != nil {
		prueba.Fatalf("crear: %v", err)
	}
	for _, estado := range []dominio.Estado{dominio.EstadoConfirmado, dominio.EstadoEnProduccion} {
		if pedido, err = actual.servicio.CambiarEstado(context.Background(), pedido.ID, estado); err != nil {
			prueba.Fatalf("pasar a %s: %v", estado, err)
		}
	}
	return pedido
}

func (actual *entorno) pedido(prueba *testing.T, id string) dominio.Pedido {
	prueba.Helper()
	pedido, err := actual.servicio.Obtener(context.Background(), "", id)
	if err != nil {
		prueba.Fatalf("obtener %s: %v", id, err)
	}
	return pedido
}

func medialunas(cantidad int, fecha string) dominio.Solicitud {
	return dominio.Solicitud{Tipo: dominio.TipoProgramado, Items: []dominio.ItemSolicitado{{ProductoID: "medialuna", Cantidad: cantidad}}, Fecha: fecha}
}

func TestCrearReservaElCupoYLosInsumosYEsperaAlVendedor(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)

	pedido, err := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(3, hoy))

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if pedido.Estado != dominio.EstadoPendienteConfirmacion || pedido.TotalCentavos != 240000 {
		prueba.Errorf("estado %s, total %d", pedido.Estado, pedido.TotalCentavos)
	}
	if reservados := actual.repositorio.reservados(hoy); reservados != 3 {
		prueba.Errorf("reservados = %d, se esperaban 3", reservados)
	}
	if harina, manteca := actual.existencias.de("harina", "g"), actual.existencias.de("manteca", "g"); harina.Reservado != 150 || manteca.Reservado != 60 {
		prueba.Errorf("reservado: harina %d (150), manteca %d (60)", harina.Reservado, manteca.Reservado)
	}
	if estados := actual.repositorio.estadosDeLosEventos(); !slices.Equal(estados, []string{"pendiente_confirmacion"}) {
		prueba.Errorf("eventos = %v", estados)
	}
}

// La reserva nunca rechaza un pedido: sin stock cargado el pedido se toma igual
// y el disponible queda negativo.
func TestCrearNoDependeDelProveedorNiDelStock(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	actual.proveedor.fallar = errCaido

	pedido, err := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy))

	if err != nil || pedido.Estado != dominio.EstadoPendienteConfirmacion {
		prueba.Fatalf("estado %s (%v): el pedido no se rechaza (R3)", pedido.Estado, err)
	}
	if actual.proveedor.consultas != 0 || len(actual.proveedor.reducciones) != 0 {
		prueba.Error("crear un pedido no llama al proveedor")
	}
	if harina := actual.existencias.de("harina", "g"); harina.Disponible() != -50 {
		prueba.Errorf("disponible de la harina = %d, se esperaba -50", harina.Disponible())
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
	if harina := actual.existencias.de("harina", "g"); harina.Reservado != 150 {
		prueba.Errorf("el reintento no puede reservar insumos de nuevo: %d", harina.Reservado)
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
		dominio.Solicitud{Tipo: dominio.TipoInmediato, Items: []dominio.ItemSolicitado{{ProductoID: "medialuna", Cantidad: 3}}})

	if err != nil || pedido.Fecha != "2026-10-28" {
		prueba.Fatalf("fecha = %s (%v), se esperaba 2026-10-28", pedido.Fecha, err)
	}
}

func TestVariosProductosReservanLaSumaDeUnidades(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	actual.capacidad(prueba, "2026-10-28", 10)

	// La torta necesita 2 días: un inmediato con torta y medialunas va al 28, no a hoy.
	pedido, err := actual.servicio.Crear(context.Background(), comprador, "clave-1", dominio.Solicitud{
		Tipo:  dominio.TipoInmediato,
		Items: []dominio.ItemSolicitado{{ProductoID: "torta", Cantidad: 1}, {ProductoID: "medialuna", Cantidad: 6}},
	})

	if err != nil || pedido.Fecha != "2026-10-28" {
		prueba.Fatalf("fecha = %s (%v), se esperaba 2026-10-28", pedido.Fecha, err)
	}
	if reservados := actual.repositorio.reservados("2026-10-28"); reservados != 7 {
		prueba.Errorf("reservados = %d, se esperaban 7", reservados)
	}
	if pedido.TotalCentavos != 1250000+6*80000 {
		prueba.Errorf("total = %d", pedido.TotalCentavos)
	}
}

func TestSinCupoNiProductoNoCreanNada(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 1)

	if _, err := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(2, hoy)); !errors.Is(err, dominio.ErrSinCupo) {
		prueba.Errorf("se esperaba ErrSinCupo, dio: %v", err)
	}
	inexistente := medialunas(1, hoy)
	inexistente.Items = append(inexistente.Items, dominio.ItemSolicitado{ProductoID: "alfajor", Cantidad: 1})
	if _, err := actual.servicio.Crear(context.Background(), comprador, "clave-2", inexistente); !errors.Is(err, dominio.ErrProductoInexistente) {
		prueba.Errorf("se esperaba ErrProductoInexistente, dio: %v", err)
	}
	if len(actual.repositorio.pedidos) != 0 {
		prueba.Errorf("no se tenía que guardar ningún pedido: %d", len(actual.repositorio.pedidos))
	}
	if harina := actual.existencias.de("harina", "g"); harina.Reservado != 0 {
		prueba.Errorf("sin pedido no hay reserva: %d", harina.Reservado)
	}
}

func TestElVendedorVeLosInsumosQueFaltanDeCadaPendiente(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	actual.existencias.conStock("harina", "g", 100)
	actual.existencias.conStock("manteca", "g", 1000)
	actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(3, hoy)) // pide 150 g de harina

	pendientes, err := actual.servicio.ListarPendientesDeConfirmar(context.Background())

	if err != nil || len(pendientes) != 1 {
		prueba.Fatalf("pendientes = %v (%v)", pendientes, err)
	}
	esperados := []dominio.Insumo{{Ingrediente: "harina", Cantidad: 50, Unidad: "g"}}
	if !slices.Equal(pendientes[0].Faltantes, esperados) {
		prueba.Errorf("faltantes = %v, se esperaba %v", pendientes[0].Faltantes, esperados)
	}

	// Cargado el stock, ya no falta nada y el vendedor puede confirmar.
	actual.existencias.conStock("harina", "g", 1000)
	pendientes, _ = actual.servicio.ListarPendientesDeConfirmar(context.Background())
	if len(pendientes[0].Faltantes) != 0 {
		prueba.Errorf("con stock no falta nada: %v", pendientes[0].Faltantes)
	}
}

func TestElVendedorConfirmaAunqueFalteStock(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	pedido, _ := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy))

	confirmado, err := actual.servicio.CambiarEstado(context.Background(), pedido.ID, dominio.EstadoConfirmado)

	if err != nil || confirmado.Estado != dominio.EstadoConfirmado {
		prueba.Fatalf("decide el vendedor: %s (%v)", confirmado.Estado, err)
	}
}

func TestCancelarDevuelveCupoYReservaSinLlamarAlProveedor(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	pedido, _ := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(4, hoy))
	actual.proveedor.fallar = errCaido

	cancelado, err := actual.servicio.Cancelar(context.Background(), comprador.ID, pedido.ID)

	if err != nil || cancelado.Estado != dominio.EstadoCancelado || cancelado.InsumosReservados {
		prueba.Fatalf("estado %s, reservados %v (%v)", cancelado.Estado, cancelado.InsumosReservados, err)
	}
	if reservados := actual.repositorio.reservados(hoy); reservados != 0 {
		prueba.Errorf("reservados = %d, se esperaba 0", reservados)
	}
	if harina := actual.existencias.de("harina", "g"); harina.Reservado != 0 || harina.Stock != 0 {
		prueba.Errorf("la reserva vuelve y el stock no se toca: %+v", harina)
	}
	if actual.proveedor.consultas != 0 || len(actual.proveedor.reducciones) != 0 {
		prueba.Error("cancelar no llama al proveedor")
	}
}

func TestPasarAProduccionNoEsperaAlProveedor(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.proveedor.fallar = errCaido
	actual.capacidad(prueba, hoy, 10)
	pedido, _ := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy))
	actual.servicio.CambiarEstado(context.Background(), pedido.ID, dominio.EstadoConfirmado)

	enProduccion, err := actual.servicio.CambiarEstado(context.Background(), pedido.ID, dominio.EstadoEnProduccion)

	if err != nil || enProduccion.Estado != dominio.EstadoEnProduccion || !enProduccion.ConsumoPendiente {
		prueba.Errorf("estado %s, consumo pendiente %v (%v)", enProduccion.Estado, enProduccion.ConsumoPendiente, err)
	}
	if len(actual.proveedor.reducciones) != 0 {
		prueba.Error("el consumo es en segundo plano, no al cambiar de estado")
	}
}

func TestElConsumoDescuentaCadaInsumoYBajaLaReserva(prueba *testing.T) {
	actual := nuevoEntorno()
	pedido := actual.pedidoEnProduccion(prueba)

	procesados, err := actual.servicio.ProcesarConsumos(context.Background())

	if err != nil || procesados != 1 {
		prueba.Fatalf("procesados %d (%v)", procesados, err)
	}
	esperadas := []llamada{{1, 50, "Pedido " + pedido.ID}, {2, 20, "Pedido " + pedido.ID}}
	if !slices.Equal(actual.proveedor.reducciones, esperadas) {
		prueba.Errorf("reducciones = %v, se esperaba %v", actual.proveedor.reducciones, esperadas)
	}
	if harina := actual.existencias.de("harina", "g"); harina.Stock != 950 || harina.Reservado != 0 || harina.ProductoID != 1 {
		prueba.Errorf("harina = %+v", harina)
	}
	if manteca := actual.existencias.de("manteca", "g"); manteca.Stock != 980 || manteca.Reservado != 0 {
		prueba.Errorf("manteca = %+v", manteca)
	}
	if consumido := actual.pedido(prueba, pedido.ID); consumido.ConsumoPendiente || consumido.InsumosReservados || consumido.RequiereRevision {
		prueba.Errorf("el consumo terminó: %+v", consumido)
	}
	if procesados, _ := actual.servicio.ProcesarConsumos(context.Background()); procesados != 0 || len(actual.proveedor.reducciones) != 2 {
		prueba.Errorf("no se descuenta dos veces: procesados %d, reducciones %d", procesados, len(actual.proveedor.reducciones))
	}
}

// Sin stock en el proveedor (409) el pedido sigue en producción y queda marcado
// para el vendedor; no se reintenta y devuelve lo que tenía reservado.
func TestSinStockEnElProveedorMarcaElPedidoParaElVendedor(prueba *testing.T) {
	actual := nuevoEntorno()
	pedido := actual.pedidoEnProduccion(prueba)
	actual.proveedor.stocks[1] = 0

	actual.servicio.ProcesarConsumos(context.Background())

	marcado := actual.pedido(prueba, pedido.ID)
	if marcado.Estado != dominio.EstadoEnProduccion || !marcado.RequiereRevision || marcado.ConsumoPendiente {
		prueba.Fatalf("sigue en producción, para revisar y sin consumo: %+v", marcado)
	}
	if marcado.InsumosReservados {
		prueba.Error("en revisión ya no tiene reserva")
	}
	harina, manteca := actual.existencias.de("harina", "g"), actual.existencias.de("manteca", "g")
	if harina.Reservado != 0 || manteca.Reservado != 0 {
		prueba.Errorf("lo que no se consumió vuelve: harina %d, manteca %d reservados", harina.Reservado, manteca.Reservado)
	}
	actual.servicio.ProcesarConsumos(context.Background())
	if len(actual.proveedor.reducciones) != 1 {
		prueba.Errorf("un 409 no se reintenta: %d reducciones", len(actual.proveedor.reducciones))
	}
}

func TestUnRechazoPorFormatoSeFrenaParaElVendedor(prueba *testing.T) {
	actual := nuevoEntorno()
	pedido := actual.pedidoEnProduccion(prueba)
	actual.proveedor.fallar = fmt.Errorf("%w: 422", puertos.ErrInventarioRechazo)

	actual.servicio.ProcesarConsumos(context.Background())
	*actual.reloj = momento.Add(time.Hour)
	actual.servicio.ProcesarConsumos(context.Background())

	if marcado := actual.pedido(prueba, pedido.ID); !marcado.RequiereRevision {
		prueba.Error("un rechazo queda para el vendedor")
	}
	if len(actual.proveedor.reducciones) != 1 || !slices.Contains(actual.eventos.inventario, puertos.InventarioRechazada) {
		prueba.Errorf("un rechazo no se reintenta y se avisa: %d reducciones, %v", len(actual.proveedor.reducciones), actual.eventos.inventario)
	}
}

// Si el proveedor aplicó el descuento pero no llegó la respuesta, el reintento
// consulta el stock y no descuenta otra vez.
func TestRespuestaPerdidaNoDescuentaDosVeces(prueba *testing.T) {
	actual := nuevoEntorno()
	pedido := actual.pedidoEnProduccion(prueba)
	actual.proveedor.fallar, actual.proveedor.aplicarAunqueFalle = errCaido, true

	actual.servicio.ProcesarConsumos(context.Background())

	enDuda := actual.pedido(prueba, pedido.ID)
	if !enDuda.ConsumoEnDuda || !enDuda.ProximoConsumo.Equal(momento.Add(time.Minute)) || enDuda.InsumosConsumidos != 0 {
		prueba.Fatalf("en duda y a reintentar en 1 minuto: %+v", enDuda)
	}
	actual.proveedor.fallar, actual.proveedor.aplicarAunqueFalle = nil, false
	*actual.reloj = momento.Add(time.Minute)

	actual.servicio.ProcesarConsumos(context.Background())

	if reducciones := actual.proveedor.reducciones; len(reducciones) != 2 || reducciones[1].producto != 2 {
		prueba.Errorf("la harina no se repite, sigue la manteca: %v", reducciones)
	}
	if harina := actual.existencias.de("harina", "g"); harina.Stock != 950 || harina.Reservado != 0 {
		prueba.Errorf("harina = %+v", harina)
	}
	if terminado := actual.pedido(prueba, pedido.ID); terminado.ConsumoPendiente || terminado.ConsumoEnDuda {
		prueba.Errorf("terminó: %+v", terminado)
	}
}

// Si la llamada no llegó al proveedor, el reintento ve que el stock no cambió y
// descuenta.
func TestRespuestaPerdidaQueNoSeAplicoSeRepite(prueba *testing.T) {
	actual := nuevoEntorno()
	pedido := actual.pedidoEnProduccion(prueba)
	actual.proveedor.fallar = errCaido
	actual.servicio.ProcesarConsumos(context.Background())
	actual.proveedor.fallar = nil
	*actual.reloj = momento.Add(time.Minute)

	actual.servicio.ProcesarConsumos(context.Background())

	if harina := actual.existencias.de("harina", "g"); harina.Stock != 950 || harina.Reservado != 0 {
		prueba.Errorf("harina = %+v", harina)
	}
	if reducciones := len(actual.proveedor.reducciones); reducciones != 3 {
		prueba.Errorf("harina dos veces (la primera no llegó) y manteca: %d reducciones", reducciones)
	}
	if terminado := actual.pedido(prueba, pedido.ID); terminado.ConsumoPendiente {
		prueba.Errorf("terminó: %+v", terminado)
	}
}

func TestStockDelProveedorQueNoCoincideQuedaParaElVendedor(prueba *testing.T) {
	actual := nuevoEntorno()
	pedido := actual.pedidoEnProduccion(prueba)
	actual.proveedor.fallar, actual.proveedor.aplicarAunqueFalle = errCaido, true
	actual.servicio.ProcesarConsumos(context.Background())
	actual.proveedor.fallar, actual.proveedor.aplicarAunqueFalle = nil, false
	actual.proveedor.stocks[1] = 900 // alguien más lo cambió por afuera
	*actual.reloj = momento.Add(time.Minute)

	actual.servicio.ProcesarConsumos(context.Background())

	if marcado := actual.pedido(prueba, pedido.ID); !marcado.RequiereRevision || marcado.ConsumoPendiente {
		prueba.Errorf("no se puede saber qué pasó: queda para el vendedor: %+v", marcado)
	}
	if len(actual.proveedor.reducciones) != 1 {
		prueba.Errorf("ante la duda no se descuenta: %d reducciones", len(actual.proveedor.reducciones))
	}
}

// Con el disyuntor abierto no se llega a llamar: no hay duda de que se aplicó algo.
func TestDisyuntorAbiertoReintentaSinDuda(prueba *testing.T) {
	actual := nuevoEntorno()
	pedido := actual.pedidoEnProduccion(prueba)
	actual.proveedor.fallar = fmt.Errorf("%w: abierto", puertos.ErrInventarioNoDisponible)

	actual.servicio.ProcesarConsumos(context.Background())

	esperando := actual.pedido(prueba, pedido.ID)
	if esperando.ConsumoEnDuda || esperando.IntentosConsumo != 1 || !esperando.ProximoConsumo.Equal(momento.Add(time.Minute)) || !esperando.ConsumoPendiente {
		prueba.Errorf("sigue pendiente, sin duda y a reintentar en 1 minuto: %+v", esperando)
	}
}

func TestIngredienteSinProductoDelProveedorQuedaParaElVendedor(prueba *testing.T) {
	actual := nuevoEntornoConProductos(1) // alcanza para la harina, no para la manteca
	pedido := actual.pedidoEnProduccion(prueba)

	actual.servicio.ProcesarConsumos(context.Background())

	marcado := actual.pedido(prueba, pedido.ID)
	if !marcado.RequiereRevision || marcado.InsumosConsumidos != 1 || marcado.MotivoRevision == "" {
		prueba.Errorf("la harina se descontó y la manteca quedó para el vendedor: %+v", marcado)
	}
}

func TestPedidoSinInsumosNoLlamaAlProveedor(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, "2026-10-28", 10)
	pedido, _ := actual.servicio.Crear(context.Background(), comprador, "clave-1", dominio.Solicitud{
		Tipo: dominio.TipoProgramado, Items: []dominio.ItemSolicitado{{ProductoID: "torta", Cantidad: 1}}, Fecha: "2026-10-28",
	})
	actual.servicio.CambiarEstado(context.Background(), pedido.ID, dominio.EstadoConfirmado)

	enProduccion, _ := actual.servicio.CambiarEstado(context.Background(), pedido.ID, dominio.EstadoEnProduccion)
	procesados, _ := actual.servicio.ProcesarConsumos(context.Background())

	if enProduccion.ConsumoPendiente || procesados != 0 || len(actual.proveedor.reducciones) != 0 {
		prueba.Errorf("sin receta no hay nada que consumir: %+v, procesados %d", enProduccion, procesados)
	}
}

func TestCargarStockSumaEnElProveedorYGuardaLoQueDevuelve(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.proveedor.stocks[1] = 40 // ya tenía algo

	existencia, err := actual.servicio.CargarStock(context.Background(), " harina ", "g", 1000)

	if err != nil || existencia.Stock != 1040 || existencia.ProductoID != 1 {
		prueba.Fatalf("existencia = %+v (%v)", existencia, err)
	}
	if guardada := actual.existencias.de("harina", "g"); guardada.Stock != 1040 {
		prueba.Errorf("se guarda el stock que devolvió el proveedor: %d", guardada.Stock)
	}
	if !slices.Contains(actual.eventos.inventario, puertos.InventarioCargada) {
		prueba.Errorf("se cuenta la carga: %v", actual.eventos.inventario)
	}
}

func TestCargarStockValidaYNoGuardaSiElProveedorFalla(prueba *testing.T) {
	actual := nuevoEntorno()
	for _, caso := range []struct {
		ingrediente, unidad string
		cantidad            int64
	}{{"", "g", 1}, {"harina", "", 1}, {"harina", "g", 0}, {"harina", "g", -5}, {"harina", "g", 2_000_000}} {
		if _, err := actual.servicio.CargarStock(context.Background(), caso.ingrediente, caso.unidad, caso.cantidad); !errors.Is(err, dominio.ErrSolicitudInvalida) {
			prueba.Errorf("%+v: se esperaba solicitud inválida, dio: %v", caso, err)
		}
	}

	actual.proveedor.fallar = fmt.Errorf("%w: abierto", puertos.ErrInventarioNoDisponible)
	if _, err := actual.servicio.CargarStock(context.Background(), "harina", "g", 100); !errors.Is(err, puertos.ErrInventarioNoDisponible) {
		prueba.Errorf("se esperaba el error del proveedor, dio: %v", err)
	}
	if guardada := actual.existencias.de("harina", "g"); guardada.Stock != 0 {
		prueba.Errorf("si el proveedor no respondió no se guarda nada: %d", guardada.Stock)
	}
}

func TestCargarStockSinProductosLibresDelProveedor(prueba *testing.T) {
	actual := nuevoEntornoConProductos()

	if _, err := actual.servicio.CargarStock(context.Background(), "harina", "g", 100); !errors.Is(err, puertos.ErrSinProductoDelProveedor) {
		prueba.Errorf("se esperaba ErrSinProductoDelProveedor, dio: %v", err)
	}
}

func TestElConsumoPendienteSeInformaALasMetricas(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.pedidoEnProduccion(prueba)
	actual.proveedor.fallar = errCaido

	actual.servicio.ProcesarConsumos(context.Background())

	if actual.eventos.pendientes != 1 {
		prueba.Errorf("pendientes = %d, se esperaba 1", actual.eventos.pendientes)
	}
}

func TestNoSeCancelaEnProduccionNiElPedidoDeOtro(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	pedido, _ := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy))

	if _, err := actual.servicio.Cancelar(context.Background(), "otro", pedido.ID); !errors.Is(err, dominio.ErrNoEncontrado) {
		prueba.Errorf("el pedido de otro comprador no existe para él, dio: %v", err)
	}
	actual.servicio.CambiarEstado(context.Background(), pedido.ID, dominio.EstadoConfirmado)
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
	pedido, _ := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy))
	actual.servicio.CambiarEstado(context.Background(), pedido.ID, dominio.EstadoConfirmado)

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

// Si el broker vuelve antes de que venza el apartado del evento que falló, el
// siguiente no se le adelanta: el comprador recibe los avisos en orden.
func TestBrokerQueVuelveRespetaElOrdenDeLosEventos(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	pedido, _ := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy))
	actual.servicio.CambiarEstado(context.Background(), pedido.ID, dominio.EstadoConfirmado)
	enOrden := []string{actual.repositorio.eventos[0].Evento.ID, actual.repositorio.eventos[1].Evento.ID}

	actual.publicador.fallar = errCaido
	actual.servicio.PublicarEventos(context.Background())

	actual.publicador.fallar = nil
	*actual.reloj = momento.Add(3 * time.Second)
	publicados, err := actual.servicio.PublicarEventos(context.Background())

	if publicados != 2 || err != nil {
		prueba.Fatalf("publicados %d (%v): al volver el broker salen los dos", publicados, err)
	}
	if actual.publicador.publicados[0] != enOrden[0] || actual.publicador.publicados[1] != enOrden[1] {
		prueba.Errorf("se publicaron %v, el orden es %v", actual.publicador.publicados, enOrden)
	}
}

func TestBaseCaidaNoCreaPedido(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	actual.repositorio.fallar = errCaido

	if _, err := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy)); !errors.Is(err, errCaido) {
		prueba.Errorf("se esperaba el error de la base, dio: %v", err)
	}
	if harina := actual.existencias.de("harina", "g"); harina.Reservado != 0 {
		prueba.Error("sin pedido guardado no se reserva nada")
	}
}

// Cada cambio guardado con evento se avisa en vivo al comprador (ADR-018).
func TestLosCambiosGuardadosSeAvisanEnVivo(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)

	pedido, err := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy))
	if err != nil {
		prueba.Fatalf("crear: %v", err)
	}
	for _, estado := range []dominio.Estado{dominio.EstadoConfirmado, dominio.EstadoEnProduccion} {
		if _, err := actual.servicio.CambiarEstado(context.Background(), pedido.ID, estado); err != nil {
			prueba.Fatalf("cambiar estado: %v", err)
		}
	}

	if estados := actual.avisos.estados(); !slices.Equal(estados, []string{"pendiente_confirmacion", "confirmado", "en_produccion"}) {
		prueba.Errorf("avisos = %v", estados)
	}
	if ultimo := actual.avisos.eventos[2].Datos; ultimo.IDComprador != comprador.ID || ultimo.IDPedido != pedido.ID || ultimo.EstadoAnterior != "confirmado" {
		prueba.Errorf("último aviso = %+v", ultimo)
	}
}

// Lo que se avisa ya está en la base: si guardar falla, no se avisa nada.
func TestSiNoSeGuardaNoSeAvisa(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, hoy, 10)
	pedido, _ := actual.servicio.Crear(context.Background(), comprador, "clave-1", medialunas(1, hoy))
	antes := len(actual.avisos.estados())
	actual.repositorio.fallarGuardar = errCaido

	if _, err := actual.servicio.CambiarEstado(context.Background(), pedido.ID, dominio.EstadoConfirmado); err == nil {
		prueba.Fatal("se esperaba el error de la base")
	}

	if despues := len(actual.avisos.estados()); despues != antes {
		prueba.Errorf("sin guardar no se avisa: había %d avisos, hay %d", antes, despues)
	}
}
