package mongodb_test

// Tests de integración contra un MongoDB real. Corren solo si está definida
// MONGO_URI_TEST. MongoDB tiene que ser un replica set, porque el repositorio usa
// transacciones (D4), por ejemplo:
//
//	docker run -d --rm --name mongo-test -p 27019:27017 mongo:8 --replSet rs0
//	docker exec mongo-test mongosh --quiet --eval "rs.initiate()"
//	MONGO_URI_TEST="mongodb://localhost:27019/?directConnection=true" go test ./...
//
// Cada test usa una base nueva y la borra al terminar.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/adaptadores/mongodb"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/puertos"
)

// MongoDB guarda las fechas con precisión de milisegundos.
var momento = time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)

const fecha = "2026-10-30"

func nuevoRepositorio(prueba *testing.T) *mongodb.Repositorio {
	prueba.Helper()
	repositorio, _ := nuevoRepositorioConExistencias(prueba)
	return repositorio
}

// nuevoRepositorioConExistencias da también las existencias, que comparten con el
// repositorio la base de inventario; los productos 1, 2 y 3 del proveedor se reparten
// entre los ingredientes.
func nuevoRepositorioConExistencias(prueba *testing.T) (*mongodb.Repositorio, *mongodb.Existencias) {
	prueba.Helper()
	direccion := os.Getenv("MONGO_URI_TEST")
	if direccion == "" {
		prueba.Skip("MONGO_URI_TEST no está definida: se saltea el test de integración")
	}

	contexto, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelar()
	cliente, err := mongo.Connect(options.Client().ApplyURI(direccion))
	if err != nil {
		prueba.Fatalf("conectar a MongoDB: %v", err)
	}
	if err := cliente.Ping(contexto, nil); err != nil {
		prueba.Fatalf("MongoDB no responde en %s: %v", direccion, err)
	}

	nombreBase := fmt.Sprintf("pedidos_test_%d", time.Now().UnixNano())
	nombreInventario := nombreBase + "_inventario"
	prueba.Cleanup(func() {
		contextoLimpieza, cancelarLimpieza := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelarLimpieza()
		cliente.Database(nombreBase).Drop(contextoLimpieza)
		cliente.Database(nombreInventario).Drop(contextoLimpieza)
		cliente.Disconnect(contextoLimpieza)
	})

	repositorio := mongodb.NuevoRepositorio(cliente, nombreBase, nombreInventario)
	if err := repositorio.PrepararIndices(contexto); err != nil {
		prueba.Fatalf("preparar índices: %v", err)
	}
	return repositorio, mongodb.NuevasExistencias(cliente, nombreInventario, []int64{1, 2, 3})
}

func pedido(id, idComprador, clave string, cantidad int) dominio.Pedido {
	return dominio.Pedido{
		ID:                id,
		Comprador:         dominio.Comprador{ID: idComprador, Email: idComprador + "@ejemplo.com"},
		Items:             []dominio.Item{{ProductoID: "torta", Nombre: "Torta", Cantidad: cantidad}},
		Fecha:             fecha,
		Tipo:              dominio.TipoProgramado,
		Estado:            dominio.EstadoPendienteConfirmacion,
		ClaveIdempotencia: clave,
		Insumos:           []dominio.Insumo{{Ingrediente: "harina", Cantidad: 500, Unidad: "g"}},
		InsumosReservados: true,
		CreadoEn:          momento,
		ActualizadoEn:     momento,
	}
}

func evento(id string, pedido dominio.Pedido) dominio.Evento {
	return dominio.NuevoEventoEstadoCambiado(id, pedido, "", momento)
}

func definir(prueba *testing.T, repositorio *mongodb.Repositorio, capacidad int) {
	prueba.Helper()
	if _, err := repositorio.DefinirCapacidad(context.Background(), fecha, capacidad); err != nil {
		prueba.Fatalf("definir capacidad: %v", err)
	}
}

func reservados(prueba *testing.T, repositorio *mongodb.Repositorio) int {
	prueba.Helper()
	dias, err := repositorio.ListarCapacidad(context.Background(), fecha, fecha)
	if err != nil || len(dias) != 1 {
		prueba.Fatalf("listar capacidad: %v (%d días)", err, len(dias))
	}
	return dias[0].Reservados
}

func contarEventos(prueba *testing.T, repositorio *mongodb.Repositorio) int {
	prueba.Helper()
	cantidad := 0
	for {
		_, encontrado, err := repositorio.TomarEvento(context.Background(), momento.Add(time.Hour), momento.Add(48*time.Hour))
		if err != nil {
			prueba.Fatalf("tomar evento: %v", err)
		}
		if !encontrado {
			return cantidad
		}
		cantidad++
	}
}

// R1: con muchos compradores a la vez, nunca se reserva más que la capacidad.
func TestNoSeSobrevendeConCompradoresSimultaneos(prueba *testing.T) {
	repositorio := nuevoRepositorio(prueba)
	definir(prueba, repositorio, 5)

	var aceptados, sinCupo atomic.Int32
	var grupo sync.WaitGroup
	for numero := range 20 {
		grupo.Go(func() {
			nuevo := pedido(fmt.Sprintf("pedido-%d", numero), fmt.Sprintf("comprador-%d", numero), "clave", 1)
			err := repositorio.Registrar(context.Background(), nuevo, evento(fmt.Sprintf("evento-%d", numero), nuevo))
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

	if aceptados.Load() != 5 || sinCupo.Load() != 15 {
		prueba.Errorf("aceptados %d, sin cupo %d: se esperaban 5 y 15", aceptados.Load(), sinCupo.Load())
	}
	if total := reservados(prueba, repositorio); total != 5 {
		prueba.Errorf("reservados = %d, se esperaban 5", total)
	}
	// Los rechazados no dejaron ni pedido ni evento: la transacción se deshizo entera.
	if eventos := contarEventos(prueba, repositorio); eventos != 5 {
		prueba.Errorf("eventos = %d, se esperaban 5", eventos)
	}
}

func TestDiaSinCapacidadDefinidaNoTieneCupo(prueba *testing.T) {
	repositorio := nuevoRepositorio(prueba)
	nuevo := pedido("pedido-1", "comprador-1", "clave", 1)

	if err := repositorio.Registrar(context.Background(), nuevo, evento("evento-1", nuevo)); !errors.Is(err, dominio.ErrSinCupo) {
		prueba.Errorf("se esperaba ErrSinCupo, dio: %v", err)
	}
	if _, err := repositorio.Buscar(context.Background(), "pedido-1"); !errors.Is(err, dominio.ErrNoEncontrado) {
		prueba.Errorf("sin cupo no queda el pedido guardado: %v", err)
	}
}

// R1: el mismo comprador con la misma clave no reserva dos veces.
func TestLaMismaClaveNoReservaDosVeces(prueba *testing.T) {
	repositorio := nuevoRepositorio(prueba)
	definir(prueba, repositorio, 10)
	primero := pedido("pedido-1", "comprador-1", "clave", 3)
	segundo := pedido("pedido-2", "comprador-1", "clave", 3)

	if err := repositorio.Registrar(context.Background(), primero, evento("evento-1", primero)); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if err := repositorio.Registrar(context.Background(), segundo, evento("evento-2", segundo)); !errors.Is(err, dominio.ErrPedidoRepetido) {
		prueba.Fatalf("se esperaba ErrPedidoRepetido, dio: %v", err)
	}
	if total := reservados(prueba, repositorio); total != 3 {
		prueba.Errorf("reservados = %d, se esperaban 3", total)
	}
	encontrado, err := repositorio.BuscarPorClave(context.Background(), "comprador-1", "clave")
	if err != nil || encontrado.ID != "pedido-1" || encontrado.Insumos[0].Cantidad != 500 {
		prueba.Errorf("buscar por clave dio %+v (%v)", encontrado, err)
	}
}

// harinaReservada es lo reservado de harina en el stock propio.
func harinaReservada(prueba *testing.T, existencias *mongodb.Existencias) int64 {
	prueba.Helper()
	todas, err := existencias.Listar(context.Background())
	if err != nil {
		prueba.Fatalf("listar existencias: %v", err)
	}
	for _, existencia := range todas {
		if existencia.Ingrediente == "harina" {
			return existencia.Reservado
		}
	}
	return 0
}

// R7: cancelar devuelve el cupo y la reserva de insumos, y guarda el evento, en la
// misma transacción.
func TestCancelarDevuelveElCupoYLaReserva(prueba *testing.T) {
	repositorio, existencias := nuevoRepositorioConExistencias(prueba)
	definir(prueba, repositorio, 10)
	nuevo := pedido("pedido-1", "comprador-1", "clave", 4)
	repositorio.Registrar(context.Background(), nuevo, evento("evento-1", nuevo))
	if reservada := harinaReservada(prueba, existencias); reservada != 500 {
		prueba.Fatalf("al registrar se reservan los insumos: %d", reservada)
	}

	anterior, _ := nuevo.CambiarEstado(dominio.EstadoCancelado, "Cancelado por el comprador.", momento)
	cancelacion := dominio.NuevoEventoEstadoCambiado("evento-2", nuevo, anterior, momento)
	err := repositorio.GuardarCambio(context.Background(), puertos.Cambio{Pedido: nuevo, VersionAnterior: 0, Evento: &cancelacion, LiberarCupo: true, LiberarReserva: true})

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if total := reservados(prueba, repositorio); total != 0 {
		prueba.Errorf("reservados = %d, se esperaba 0", total)
	}
	if reservada := harinaReservada(prueba, existencias); reservada != 0 {
		prueba.Errorf("la reserva de insumos tenía que volver: %d", reservada)
	}
	guardado, _ := repositorio.Buscar(context.Background(), "pedido-1")
	if guardado.Estado != dominio.EstadoCancelado || guardado.Version != 1 || guardado.InsumosReservados {
		prueba.Errorf("pedido guardado: %+v", guardado)
	}
	if eventos := contarEventos(prueba, repositorio); eventos != 2 {
		prueba.Errorf("eventos = %d, se esperaban 2", eventos)
	}
}

func TestElMensajeDelVendedorSeGuarda(prueba *testing.T) {
	repositorio := nuevoRepositorio(prueba)
	especial, _ := dominio.NuevoEspecial("especial-1", dominio.Comprador{ID: "comprador-1", Email: "comprador-1@ejemplo.com"}, "clave", "Torta de seis pisos", "2026-10-30", momento)
	repositorio.Registrar(context.Background(), especial, evento("evento-1", especial))

	anterior, _ := especial.RechazarSolicitud("No llegamos para esa fecha.", momento)
	rechazo := dominio.NuevoEventoEstadoCambiado("evento-2", especial, anterior, momento)
	if err := repositorio.GuardarCambio(context.Background(), puertos.Cambio{Pedido: especial, VersionAnterior: 0, Evento: &rechazo}); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}

	guardado, _ := repositorio.Buscar(context.Background(), "especial-1")
	if guardado.Estado != dominio.EstadoCancelado || guardado.MensajeVendedor != "No llegamos para esa fecha." {
		prueba.Errorf("pedido guardado: %+v", guardado)
	}
}

// Dos cambios hechos sobre la misma versión: el segundo no pisa al primero, y su
// cupo y su evento tampoco se guardan.
func TestCambioSobreVersionViejaNoSeGuarda(prueba *testing.T) {
	repositorio := nuevoRepositorio(prueba)
	definir(prueba, repositorio, 10)
	nuevo := pedido("pedido-1", "comprador-1", "clave", 4)
	repositorio.Registrar(context.Background(), nuevo, evento("evento-1", nuevo))

	confirmado := nuevo
	confirmado.CambiarEstado(dominio.EstadoConfirmado, "", momento)
	if err := repositorio.GuardarCambio(context.Background(), puertos.Cambio{Pedido: confirmado, VersionAnterior: 0}); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}

	cancelado := nuevo
	cancelado.CambiarEstado(dominio.EstadoCancelado, "", momento)
	cancelacion := dominio.NuevoEventoEstadoCambiado("evento-2", cancelado, dominio.EstadoPendienteConfirmacion, momento)
	err := repositorio.GuardarCambio(context.Background(), puertos.Cambio{Pedido: cancelado, VersionAnterior: 0, Evento: &cancelacion, LiberarCupo: true})

	if !errors.Is(err, dominio.ErrModificadoAlMismoTiempo) {
		prueba.Fatalf("se esperaba ErrModificadoAlMismoTiempo, dio: %v", err)
	}
	if total := reservados(prueba, repositorio); total != 4 {
		prueba.Errorf("el cupo no se libera si el cambio no se guardó: reservados = %d", total)
	}
	if eventos := contarEventos(prueba, repositorio); eventos != 1 {
		prueba.Errorf("eventos = %d, se esperaba solo el de la creación", eventos)
	}
}

// VE3: la capacidad no baja de lo reservado.
func TestCapacidadNoBajaDeLoReservado(prueba *testing.T) {
	repositorio := nuevoRepositorio(prueba)
	definir(prueba, repositorio, 10)
	nuevo := pedido("pedido-1", "comprador-1", "clave", 6)
	repositorio.Registrar(context.Background(), nuevo, evento("evento-1", nuevo))

	dia, err := repositorio.DefinirCapacidad(context.Background(), fecha, 5)
	if !errors.Is(err, dominio.ErrCapacidadMenorQueReservados) || dia.Reservados != 6 {
		prueba.Fatalf("se esperaba ErrCapacidadMenorQueReservados con 6 reservados, dio %+v (%v)", dia, err)
	}
	dia, err = repositorio.DefinirCapacidad(context.Background(), fecha, 6)
	if err != nil || dia.Capacidad != 6 || dia.Reservados != 6 {
		prueba.Errorf("bajar justo a lo reservado se permite: %+v (%v)", dia, err)
	}
}

func TestTomarParaConsumirApartaUnaSolaVez(prueba *testing.T) {
	repositorio := nuevoRepositorio(prueba)
	definir(prueba, repositorio, 10)
	nuevo := pedido("pedido-1", "comprador-1", "clave", 1)
	repositorio.Registrar(context.Background(), nuevo, evento("evento-1", nuevo))
	if _, encontrado, _ := repositorio.TomarParaConsumir(context.Background(), momento, momento.Add(2*time.Minute)); encontrado {
		prueba.Fatal("un pedido que no entró en producción no tiene nada que consumir")
	}
	enProduccion := nuevo
	enProduccion.CambiarEstado(dominio.EstadoConfirmado, "", momento)
	enProduccion.CambiarEstado(dominio.EstadoEnProduccion, "", momento)
	if err := repositorio.GuardarCambio(context.Background(), puertos.Cambio{Pedido: enProduccion, VersionAnterior: 0}); err != nil {
		prueba.Fatalf("guardar: %v", err)
	}

	tomado, encontrado, err := repositorio.TomarParaConsumir(context.Background(), momento, momento.Add(2*time.Minute))
	if err != nil || !encontrado || tomado.Version != 2 {
		prueba.Fatalf("tomado %+v, encontrado %v (%v)", tomado, encontrado, err)
	}
	if _, encontrado, _ := repositorio.TomarParaConsumir(context.Background(), momento, momento.Add(2*time.Minute)); encontrado {
		prueba.Error("un pedido apartado no se toma de nuevo antes de que venza el apartado")
	}
	if pendientes, err := repositorio.ContarConsumosPendientes(context.Background()); err != nil || pendientes != 1 {
		prueba.Errorf("pendientes = %d (%v), se esperaba 1", pendientes, err)
	}
}

func TestListarPendientesDeConfirmar(prueba *testing.T) {
	repositorio := nuevoRepositorio(prueba)
	definir(prueba, repositorio, 10)
	primero := pedido("pedido-1", "comprador-1", "clave-1", 1)
	segundo := pedido("pedido-2", "comprador-1", "clave-2", 1)
	segundo.CreadoEn = momento.Add(time.Minute)
	repositorio.Registrar(context.Background(), primero, evento("evento-1", primero))
	repositorio.Registrar(context.Background(), segundo, evento("evento-2", segundo))
	confirmado := primero
	confirmado.CambiarEstado(dominio.EstadoConfirmado, "", momento)
	repositorio.GuardarCambio(context.Background(), puertos.Cambio{Pedido: confirmado, VersionAnterior: 0})

	pendientes, err := repositorio.ListarPendientesDeConfirmar(context.Background())

	if err != nil || len(pendientes) != 1 || pendientes[0].ID != "pedido-2" {
		prueba.Errorf("solo el segundo espera al vendedor: %v (%v)", pendientes, err)
	}
}

// El consumo deja como stock el que informó el proveedor y baja lo reservado, a la vez
// que se guarda el avance del pedido.
func TestElConsumoActualizaElStockYLaReserva(prueba *testing.T) {
	repositorio, existencias := nuevoRepositorioConExistencias(prueba)
	definir(prueba, repositorio, 10)
	nuevo := pedido("pedido-1", "comprador-1", "clave", 1)
	repositorio.Registrar(context.Background(), nuevo, evento("evento-1", nuevo))
	nuevo.InsumoConsumido(momento)

	err := repositorio.GuardarCambio(context.Background(), puertos.Cambio{
		Pedido: nuevo, VersionAnterior: 0,
		Consumo: &puertos.Consumo{Insumo: dominio.Insumo{Ingrediente: "harina", Cantidad: 500, Unidad: "g"}, StockQueda: 1500},
	})

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	todas, _ := existencias.Listar(context.Background())
	if len(todas) != 1 || todas[0].Stock != 1500 || todas[0].Reservado != 0 {
		prueba.Errorf("existencias = %+v", todas)
	}
}

func TestLaReservaNoRechazaAunqueNoHayaStock(prueba *testing.T) {
	repositorio, existencias := nuevoRepositorioConExistencias(prueba)
	definir(prueba, repositorio, 10)
	nuevo := pedido("pedido-1", "comprador-1", "clave", 1)

	if err := repositorio.Registrar(context.Background(), nuevo, evento("evento-1", nuevo)); err != nil {
		prueba.Fatalf("la reserva nunca rechaza un pedido: %v", err)
	}

	harina, err := existencias.Obtener(context.Background(), "harina", "g")
	if err != nil || harina.Stock != 0 || harina.Reservado != 500 || harina.Disponible() != -500 {
		prueba.Errorf("harina = %+v (%v)", harina, err)
	}
}

func TestLosProductosDelProveedorSeRepartenSinRepetir(prueba *testing.T) {
	_, existencias := nuevoRepositorioConExistencias(prueba)
	contexto := context.Background()

	var asignados []int64
	for _, ingrediente := range []string{"harina", "manteca", "azúcar"} {
		existencia, err := existencias.AsignarProducto(contexto, ingrediente, "g")
		if err != nil {
			prueba.Fatalf("asignar %s: %v", ingrediente, err)
		}
		asignados = append(asignados, existencia.ProductoID)
	}
	if asignados[0] != 1 || asignados[1] != 2 || asignados[2] != 3 {
		prueba.Errorf("asignados = %v, se esperaba 1, 2 y 3", asignados)
	}
	otra, err := existencias.AsignarProducto(contexto, "harina", "g")
	if err != nil || otra.ProductoID != 1 {
		prueba.Errorf("pedir de nuevo devuelve el mismo producto: %d (%v)", otra.ProductoID, err)
	}
	if _, err := existencias.AsignarProducto(contexto, "huevos", "u"); !errors.Is(err, puertos.ErrSinProductoDelProveedor) {
		prueba.Errorf("sin productos libres se esperaba ErrSinProductoDelProveedor, dio: %v", err)
	}
}

func TestAsignacionesSimultaneasNoComparteProducto(prueba *testing.T) {
	_, existencias := nuevoRepositorioConExistencias(prueba)
	var grupo sync.WaitGroup
	productos := make([]int64, 3)
	for posicion, ingrediente := range []string{"harina", "manteca", "azúcar"} {
		grupo.Go(func() {
			existencia, err := existencias.AsignarProducto(context.Background(), ingrediente, "g")
			if err != nil {
				prueba.Errorf("asignar %s: %v", ingrediente, err)
			}
			productos[posicion] = existencia.ProductoID
		})
	}
	grupo.Wait()

	vistos := map[int64]bool{}
	for _, producto := range productos {
		if producto == 0 || vistos[producto] {
			prueba.Fatalf("cada ingrediente tiene que quedarse con un producto distinto: %v", productos)
		}
		vistos[producto] = true
	}
}

func TestBandejaDeSalida(prueba *testing.T) {
	repositorio := nuevoRepositorio(prueba)
	definir(prueba, repositorio, 10)
	nuevo := pedido("pedido-1", "comprador-1", "clave", 1)
	repositorio.Registrar(context.Background(), nuevo, evento("evento-1", nuevo))

	guardado, encontrado, err := repositorio.TomarEvento(context.Background(), momento, momento.Add(2*time.Minute))
	if err != nil || !encontrado || guardado.Evento.Datos.EmailComprador != "comprador-1@ejemplo.com" {
		prueba.Fatalf("evento %+v, encontrado %v (%v)", guardado, encontrado, err)
	}
	if _, encontrado, _ := repositorio.TomarEvento(context.Background(), momento, momento.Add(2*time.Minute)); encontrado {
		prueba.Fatal("un evento apartado no se toma de nuevo")
	}
	if err := repositorio.DevolverEvento(context.Background(), "evento-1", momento); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if _, encontrado, _ := repositorio.TomarEvento(context.Background(), momento, momento.Add(2*time.Minute)); !encontrado {
		prueba.Fatal("un evento devuelto se puede tomar enseguida")
	}
	if err := repositorio.MarcarPublicado(context.Background(), "evento-1"); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if _, encontrado, _ := repositorio.TomarEvento(context.Background(), momento.Add(time.Hour), momento.Add(2*time.Hour)); encontrado {
		prueba.Error("un evento publicado sale de la bandeja")
	}
}

// El cupo se reserva por la suma de unidades de todos los ítems.
func TestVariosItemsReservanLaSumaDeUnidades(prueba *testing.T) {
	repositorio := nuevoRepositorio(prueba)
	definir(prueba, repositorio, 4)
	nuevo := pedido("pedido-1", "comprador-1", "clave", 3)
	nuevo.Items = append(nuevo.Items, dominio.Item{ProductoID: "medialuna", Nombre: "Medialuna", Cantidad: 2})

	if err := repositorio.Registrar(context.Background(), nuevo, evento("evento-1", nuevo)); !errors.Is(err, dominio.ErrSinCupo) {
		prueba.Fatalf("5 unidades no entran en 4 de capacidad: %v", err)
	}
	definir(prueba, repositorio, 5)
	if err := repositorio.Registrar(context.Background(), nuevo, evento("evento-1", nuevo)); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if total := reservados(prueba, repositorio); total != 5 {
		prueba.Errorf("reservados = %d, se esperaban 5", total)
	}
	guardado, _ := repositorio.Buscar(context.Background(), "pedido-1")
	if len(guardado.Items) != 2 || guardado.Items[1].Nombre != "Medialuna" {
		prueba.Errorf("ítems guardados = %+v", guardado.Items)
	}
}
