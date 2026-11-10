package dominio_test

import (
	"errors"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
)

// momento es el 26/10/2026 a las 12:00 en Argentina.
var momento = time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)

var comprador = dominio.Comprador{ID: "comprador-1", Email: "cliente@ejemplo.com"}

func torta() dominio.Producto {
	return dominio.Producto{
		ID: "torta", Nombre: "Torta de chocolate", PrecioCentavos: 1250000, AnticipacionDias: 3,
		Receta: []dominio.Insumo{{Ingrediente: "harina", Cantidad: 500, Unidad: "g"}},
	}
}

func medialuna() dominio.Producto {
	return dominio.Producto{
		ID: "medialuna", Nombre: "Medialuna", PrecioCentavos: 80000, AnticipacionDias: 0,
		Receta: []dominio.Insumo{{Ingrediente: "harina", Cantidad: 50, Unidad: "g"}, {Ingrediente: "manteca", Cantidad: 20, Unidad: "g"}},
	}
}

func catalogo() map[string]dominio.Producto {
	return map[string]dominio.Producto{"torta": torta(), "medialuna": medialuna()}
}

func items(pares ...any) []dominio.ItemSolicitado {
	var resultado []dominio.ItemSolicitado
	for posicion := 0; posicion < len(pares); posicion += 2 {
		resultado = append(resultado, dominio.ItemSolicitado{ProductoID: pares[posicion].(string), Cantidad: pares[posicion+1].(int)})
	}
	return resultado
}

func programado(fecha string) dominio.Solicitud {
	return dominio.Solicitud{Tipo: dominio.TipoProgramado, Items: items("torta", 2), Fecha: fecha}
}

func inmediato(pares ...any) dominio.Solicitud {
	return dominio.Solicitud{Tipo: dominio.TipoInmediato, Items: items(pares...)}
}

func pedidoNuevo(prueba *testing.T) dominio.Pedido {
	prueba.Helper()
	pedido, err := dominio.NuevoPedido("pedido-1", comprador, "clave-1", programado("2026-10-30"), catalogo(), "2026-10-30", momento)
	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	return pedido
}

func TestHoyUsaLaHoraDeArgentina(prueba *testing.T) {
	// 01:00 UTC del 27 son las 22:00 del 26 en Argentina.
	if hoy := dominio.Hoy(time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)); hoy != "2026-10-26" {
		prueba.Errorf("hoy = %s, se esperaba 2026-10-26", hoy)
	}
	if fecha := dominio.SumarDias("2026-12-30", 3); fecha != "2027-01-02" {
		prueba.Errorf("sumar días cruzando el año dio %s", fecha)
	}
}

func TestValidarSolicitud(prueba *testing.T) {
	casos := []struct {
		nombre    string
		solicitud dominio.Solicitud
		esValida  bool
	}{
		{"programado completo", programado("2026-10-30"), true},
		{"inmediato con dos productos", inmediato("torta", 1, "medialuna", 12), true},
		{"programado sin fecha", programado(""), false},
		{"fecha con otro formato", programado("30/10/2026"), false},
		{"tipo desconocido", dominio.Solicitud{Tipo: "urgente", Items: items("torta", 1)}, false},
		{"sin ítems", inmediato(), false},
		{"más de 20 ítems", dominio.Solicitud{Tipo: dominio.TipoInmediato, Items: make([]dominio.ItemSolicitado, 21)}, false},
		{"ítem sin producto", inmediato("", 1), false},
		{"producto repetido", inmediato("torta", 1, "torta", 2), false},
		{"cantidad cero", inmediato("torta", 0), false},
		{"cantidad excesiva", inmediato("torta", 101), false},
	}
	for _, caso := range casos {
		prueba.Run(caso.nombre, func(prueba *testing.T) {
			err := caso.solicitud.Validar()
			if caso.esValida && err != nil {
				prueba.Fatalf("se esperaba válida, dio: %v", err)
			}
			if !caso.esValida && !errors.Is(err, dominio.ErrSolicitudInvalida) {
				prueba.Fatalf("se esperaba ErrSolicitudInvalida, dio: %v", err)
			}
		})
	}
}

func TestHuellaDistingueElContenido(prueba *testing.T) {
	if programado("2026-10-30").Huella() != programado("2026-10-30").Huella() {
		prueba.Error("la misma solicitud tiene que dar la misma huella")
	}
	if programado("2026-10-30").Huella() == programado("2026-10-31").Huella() {
		prueba.Error("otra fecha tiene que dar otra huella")
	}
	if inmediato("torta", 1, "medialuna", 2).Huella() == inmediato("torta", 2, "medialuna", 1).Huella() {
		prueba.Error("otras cantidades tienen que dar otra huella")
	}
}

func TestNuevoPedidoFijaElPrecioEnCentavos(prueba *testing.T) {
	pedido := pedidoNuevo(prueba)

	if pedido.TotalCentavos != 2500000 || pedido.Items[0].PrecioUnitarioCentavos != 1250000 || pedido.Items[0].Nombre != "Torta de chocolate" {
		prueba.Errorf("total = %d, ítems = %+v", pedido.TotalCentavos, pedido.Items)
	}
	if pedido.Estado != dominio.EstadoPendienteConfirmacion {
		prueba.Errorf("estado = %s", pedido.Estado)
	}
	if len(pedido.Insumos) != 1 || pedido.Insumos[0].Cantidad != 1000 {
		prueba.Errorf("los insumos se multiplican por la cantidad: %+v", pedido.Insumos)
	}
}

func TestPedidoConVariosProductos(prueba *testing.T) {
	solicitud := dominio.Solicitud{Tipo: dominio.TipoProgramado, Items: items("torta", 1, "medialuna", 12), Fecha: "2026-10-30"}

	pedido, err := dominio.NuevoPedido("pedido-1", comprador, "clave-1", solicitud, catalogo(), "2026-10-30", momento)

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if pedido.Unidades() != 13 || pedido.TotalCentavos != 1250000+12*80000 {
		prueba.Errorf("unidades %d, total %d", pedido.Unidades(), pedido.TotalCentavos)
	}
	if len(pedido.Items) != 2 || pedido.Items[1].SubtotalCentavos != 960000 {
		prueba.Errorf("ítems = %+v", pedido.Items)
	}
	// La harina de los dos productos se suma: 500 g + 12 × 50 g.
	esperados := []dominio.Insumo{{Ingrediente: "harina", Cantidad: 1100, Unidad: "g"}, {Ingrediente: "manteca", Cantidad: 240, Unidad: "g"}}
	if len(pedido.Insumos) != 2 || pedido.Insumos[0] != esperados[0] || pedido.Insumos[1] != esperados[1] {
		prueba.Errorf("insumos = %+v", pedido.Insumos)
	}
}

func TestLaAnticipacionEsLaDelProductoQueMasNecesita(prueba *testing.T) {
	// Las medialunas se pueden pedir para hoy, pero con una torta en el pedido hay que esperar 3 días.
	solicitud := dominio.Solicitud{Tipo: dominio.TipoProgramado, Items: items("medialuna", 6, "torta", 1), Fecha: "2026-10-27"}

	_, err := dominio.NuevoPedido("pedido-1", comprador, "clave-1", solicitud, catalogo(), "2026-10-27", momento)

	var anticipacion dominio.ErrorDeAnticipacion
	if !errors.As(err, &anticipacion) || anticipacion.PrimeraFecha != "2026-10-29" {
		prueba.Errorf("se esperaba la primera fecha 2026-10-29, dio: %v", err)
	}
}

func TestNuevoPedidoRespetaLaAnticipacionMinima(prueba *testing.T) {
	// Hoy es el 26 y la torta necesita 3 días: la primera fecha posible es el 29.
	_, err := dominio.NuevoPedido("pedido-1", comprador, "clave-1", programado("2026-10-28"), catalogo(), "2026-10-28", momento)

	var anticipacion dominio.ErrorDeAnticipacion
	if !errors.As(err, &anticipacion) || !errors.Is(err, dominio.ErrAnticipacionInsuficiente) {
		prueba.Fatalf("se esperaba ErrorDeAnticipacion, dio: %v", err)
	}
	if anticipacion.PrimeraFecha != "2026-10-29" {
		prueba.Errorf("primera fecha = %s, se esperaba 2026-10-29", anticipacion.PrimeraFecha)
	}

	if _, err := dominio.NuevoPedido("pedido-1", comprador, "clave-1", programado("2026-10-29"), catalogo(), "2026-10-29", momento); err != nil {
		prueba.Errorf("justo en la primera fecha posible se acepta: %v", err)
	}
}

func TestTransicionesDeEstado(prueba *testing.T) {
	caminoCompleto := []dominio.Estado{dominio.EstadoConfirmado, dominio.EstadoEnProduccion, dominio.EstadoListo, dominio.EstadoEntregado}
	pedido := pedidoNuevo(prueba)
	for _, estado := range caminoCompleto {
		if _, err := pedido.CambiarEstado(estado, "", momento); err != nil {
			prueba.Fatalf("pasar a %s: %v", estado, err)
		}
	}

	invalidas := []struct {
		hasta []dominio.Estado
		nuevo dominio.Estado
	}{
		{nil, dominio.EstadoListo},                                            // pendiente -> listo
		{caminoCompleto[:2], dominio.EstadoCancelado},                         // en producción no se cancela
		{caminoCompleto[:1], dominio.EstadoEntregado},                         // no se entrega sin estar listo
		{caminoCompleto, dominio.EstadoCancelado},                             // entregado es final
		{[]dominio.Estado{dominio.EstadoCancelado}, dominio.EstadoConfirmado}, // cancelado es final
	}
	for _, caso := range invalidas {
		pedido := pedidoNuevo(prueba)
		for _, estado := range caso.hasta {
			pedido.CambiarEstado(estado, "", momento)
		}
		anterior := pedido.Estado
		if _, err := pedido.CambiarEstado(caso.nuevo, "", momento); !errors.Is(err, dominio.ErrTransicionInvalida) {
			prueba.Errorf("de %s a %s se esperaba ErrTransicionInvalida, dio: %v", anterior, caso.nuevo, err)
		}
		if pedido.Estado != anterior {
			prueba.Errorf("una transición inválida no cambia el estado: quedó %s", pedido.Estado)
		}
	}
}

// en_camino es opcional, entre listo y entregado: el vendedor lo usa para los envíos, y un
// pedido que se retira en el local sigue pasando de listo a entregado.
func TestEnCaminoEsUnPasoOpcionalDelEnvio(prueba *testing.T) {
	hastaListo := []dominio.Estado{dominio.EstadoConfirmado, dominio.EstadoEnProduccion, dominio.EstadoListo}

	enviado := pedidoNuevo(prueba)
	for _, estado := range append(hastaListo, dominio.EstadoEnCamino, dominio.EstadoEntregado) {
		if _, err := enviado.CambiarEstado(estado, "", momento); err != nil {
			prueba.Fatalf("envío: pasar a %s: %v", estado, err)
		}
	}
	retirado := pedidoNuevo(prueba)
	for _, estado := range append(hastaListo, dominio.EstadoEntregado) {
		if _, err := retirado.CambiarEstado(estado, "", momento); err != nil {
			prueba.Fatalf("retiro: pasar a %s: %v", estado, err)
		}
	}

	invalidas := []struct {
		hasta []dominio.Estado
		nuevo dominio.Estado
	}{
		{hastaListo[:2], dominio.EstadoEnCamino},                              // en producción todavía no sale
		{append(hastaListo, dominio.EstadoEnCamino), dominio.EstadoCancelado}, // en camino no se cancela
		{append(hastaListo, dominio.EstadoEnCamino), dominio.EstadoListo},     // no vuelve atrás
	}
	for _, caso := range invalidas {
		pedido := pedidoNuevo(prueba)
		for _, estado := range caso.hasta {
			pedido.CambiarEstado(estado, "", momento)
		}
		if _, err := pedido.CambiarEstado(caso.nuevo, "", momento); !errors.Is(err, dominio.ErrTransicionInvalida) {
			prueba.Errorf("de %s a %s se esperaba ErrTransicionInvalida, dio: %v", pedido.Estado, caso.nuevo, err)
		}
	}
}

func TestUnPedidoConRecetaNaceConLosInsumosReservados(prueba *testing.T) {
	pedido := pedidoNuevo(prueba)

	if !pedido.InsumosReservados || pedido.ConsumoPendiente {
		prueba.Errorf("tenía que nacer con la reserva y sin consumo pendiente: %+v", pedido)
	}
}

func TestCancelarDevuelveLaReservaSinConsumirNada(prueba *testing.T) {
	pedido := pedidoNuevo(prueba)

	anterior, err := pedido.CambiarEstado(dominio.EstadoCancelado, "Cancelado por el comprador.", momento)

	if err != nil || anterior != dominio.EstadoPendienteConfirmacion {
		prueba.Fatalf("anterior = %s, error = %v", anterior, err)
	}
	if pedido.InsumosReservados || pedido.ConsumoPendiente || pedido.MotivoCancelacion != "Cancelado por el comprador." {
		prueba.Errorf("se esperaba sin reserva, sin consumo y con el motivo: %+v", pedido)
	}
}

func TestPasarAProduccionDejaElConsumoPendiente(prueba *testing.T) {
	pedido := pedidoNuevo(prueba)
	pedido.CambiarEstado(dominio.EstadoConfirmado, "", momento)

	pedido.CambiarEstado(dominio.EstadoEnProduccion, "", momento)

	insumo, hay := pedido.InsumoPorConsumir()
	if !pedido.ConsumoPendiente || !pedido.ProximoConsumo.Equal(momento) || !hay || insumo.Ingrediente != "harina" {
		prueba.Errorf("el consumo tenía que quedar pendiente desde ya, con la harina primero: %+v", pedido)
	}
}

func TestUnPedidoSinInsumosNoTieneNadaQueConsumir(prueba *testing.T) {
	pedido := pedidoNuevo(prueba)
	pedido.Insumos, pedido.InsumosReservados = nil, false
	pedido.CambiarEstado(dominio.EstadoConfirmado, "", momento)

	pedido.CambiarEstado(dominio.EstadoEnProduccion, "", momento)

	if pedido.ConsumoPendiente {
		prueba.Error("sin insumos no se llama al proveedor")
	}
}

func TestElConsumoAvanzaInsumoPorInsumoYTermina(prueba *testing.T) {
	pedido := pedidoNuevo(prueba)
	pedido.Insumos = append(pedido.Insumos, dominio.Insumo{Ingrediente: "manteca", Cantidad: 100, Unidad: "g"})
	pedido.CambiarEstado(dominio.EstadoConfirmado, "", momento)
	pedido.CambiarEstado(dominio.EstadoEnProduccion, "", momento)
	pedido.RegistrarFalloDeConsumo(true, momento)

	pedido.InsumoConsumido(momento)

	siguiente, hay := pedido.InsumoPorConsumir()
	if !hay || siguiente.Ingrediente != "manteca" || pedido.IntentosConsumo != 0 || pedido.ConsumoEnDuda {
		prueba.Fatalf("seguía la manteca, con los intentos y la duda en cero: %+v", pedido)
	}
	pedido.InsumoConsumido(momento)
	if _, hay := pedido.InsumoPorConsumir(); hay || pedido.ConsumoPendiente || pedido.InsumosReservados {
		prueba.Errorf("con el último insumo termina el consumo y no queda reserva: %+v", pedido)
	}
}

func TestElConsumoSeReintentaConEsperasQueSeDuplican(prueba *testing.T) {
	pedido := pedidoNuevo(prueba)
	esperas := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute}

	for intento, espera := range esperas {
		pedido.RegistrarFalloDeConsumo(false, momento)
		if !pedido.ProximoConsumo.Equal(momento.Add(espera)) {
			prueba.Errorf("intento %d: próximo a los %v, se esperaba a los %v", intento+1, pedido.ProximoConsumo.Sub(momento), espera)
		}
	}
	if pedido.ConsumoEnDuda {
		prueba.Error("un fallo sin duda no marca el consumo en duda")
	}
	pedido.RegistrarFalloDeConsumo(true, momento)
	if !pedido.ConsumoEnDuda {
		prueba.Error("un timeout deja el consumo en duda")
	}
}

func TestMarcarParaRevisionFrenaElConsumoSinCancelar(prueba *testing.T) {
	pedido := pedidoNuevo(prueba)
	pedido.CambiarEstado(dominio.EstadoConfirmado, "", momento)
	pedido.CambiarEstado(dominio.EstadoEnProduccion, "", momento)

	pedido.MarcarParaRevision("Falta harina.", momento)

	if !pedido.RequiereRevision || pedido.ConsumoPendiente || pedido.Estado != dominio.EstadoEnProduccion {
		prueba.Errorf("queda para el vendedor, sin consumo y sin frenar la producción: %+v", pedido)
	}
}

func TestFaltanteCuentaLoQueLaReservaDejoSinCubrir(prueba *testing.T) {
	casos := []struct {
		nombre     string
		existencia dominio.Existencia
		necesario  int64
		esperado   int64
		disponible int64
	}{
		{"alcanza", dominio.Existencia{Stock: 1000, Reservado: 500}, 500, 0, 500},
		{"falta una parte", dominio.Existencia{Stock: 300, Reservado: 500}, 500, 200, -200},
		{"no hay nada", dominio.Existencia{Stock: 0, Reservado: 1500}, 500, 500, -1500},
	}
	for _, caso := range casos {
		if caso.existencia.Disponible() != caso.disponible || caso.existencia.Faltante(caso.necesario) != caso.esperado {
			prueba.Errorf("%s: disponible %d, faltante %d", caso.nombre, caso.existencia.Disponible(), caso.existencia.Faltante(caso.necesario))
		}
	}
}

func TestEventoLlevaLoNecesarioParaAvisar(prueba *testing.T) {
	pedido := pedidoNuevo(prueba)
	anterior, _ := pedido.CambiarEstado(dominio.EstadoConfirmado, "", momento)

	evento := dominio.NuevoEventoEstadoCambiado("evento-1", pedido, anterior, momento)

	if evento.Tipo != dominio.TipoEstadoCambiado || evento.Version != 1 {
		prueba.Errorf("tipo %s versión %d", evento.Tipo, evento.Version)
	}
	datos := evento.Datos
	if datos.EmailComprador != comprador.Email || datos.EstadoAnterior != "pendiente_confirmacion" || datos.EstadoNuevo != "confirmado" || datos.Items[0].Cantidad != 2 {
		prueba.Errorf("datos = %+v", datos)
	}
}

// Los mismos ítems en otro orden son el mismo pedido: un reintento no puede dar
// conflicto de idempotencia por eso (#93).
func TestLaHuellaNoDependeDelOrdenDeLosItems(prueba *testing.T) {
	item := func(id string, cantidad int) dominio.ItemSolicitado {
		return dominio.ItemSolicitado{ProductoID: id, Cantidad: cantidad}
	}
	solicitud := dominio.Solicitud{Tipo: dominio.TipoProgramado, Fecha: "2026-11-10", Franja: "mañana", Items: []dominio.ItemSolicitado{item("a", 1), item("b", 2)}}
	invertida := solicitud
	invertida.Items = []dominio.ItemSolicitado{item("b", 2), item("a", 1)}
	otraCantidad := solicitud
	otraCantidad.Items = []dominio.ItemSolicitado{item("a", 2), item("b", 1)}

	if solicitud.Huella() != invertida.Huella() {
		prueba.Error("el mismo contenido en otro orden tiene que dar la misma huella")
	}
	if solicitud.Huella() == otraCantidad.Huella() {
		prueba.Error("otro contenido tiene que dar otra huella")
	}
	if solicitud.Items[0].ProductoID != "a" || invertida.Items[0].ProductoID != "b" {
		prueba.Error("calcular la huella no puede cambiar el orden de la solicitud")
	}
}
