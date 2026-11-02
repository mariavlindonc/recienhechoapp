package aplicacion_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/pedidos/internal/dominio"
)

const fechaEspecial = "2026-11-05"

func (actual *entorno) especialCotizado(prueba *testing.T) dominio.Pedido {
	prueba.Helper()
	contexto := context.Background()
	especial, err := actual.servicio.SolicitarEspecial(contexto, comprador, "clave-especial", "Torta de casamiento", fechaEspecial)
	if err != nil {
		prueba.Fatalf("solicitar: %v", err)
	}
	cotizado, err := actual.servicio.CotizarEspecial(contexto, especial.ID, 9000000)
	if err != nil {
		prueba.Fatalf("cotizar: %v", err)
	}
	return cotizado
}

func TestEspecialAceptadoReservaCupoYSeConfirma(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, fechaEspecial, 1)

	cotizado := actual.especialCotizado(prueba)
	if reservados := actual.repositorio.reservados(fechaEspecial); reservados != 0 {
		prueba.Fatalf("antes de aceptar no se reserva cupo; reservados = %d", reservados)
	}
	aceptado, err := actual.servicio.ResponderEspecial(context.Background(), comprador.ID, cotizado.ID, true)

	if err != nil || aceptado.Estado != dominio.EstadoConfirmado || aceptado.TotalCentavos != 9000000 {
		prueba.Fatalf("estado %s, total %d, err %v", aceptado.Estado, aceptado.TotalCentavos, err)
	}
	if reservados := actual.repositorio.reservados(fechaEspecial); reservados != 1 {
		prueba.Errorf("reservados = %d, se esperaba 1", reservados)
	}
	if estados := actual.repositorio.estadosDeLosEventos(); !slices.Equal(estados, []string{"solicitado", "cotizado", "pendiente_confirmacion", "confirmado"}) {
		prueba.Errorf("eventos = %v", estados)
	}
}

func TestEspecialSinCupoAlAceptarNoCambia(prueba *testing.T) {
	actual := nuevoEntorno()
	cotizado := actual.especialCotizado(prueba) // el día no tiene capacidad

	_, err := actual.servicio.ResponderEspecial(context.Background(), comprador.ID, cotizado.ID, true)

	if !errors.Is(err, dominio.ErrSinCupo) {
		prueba.Fatalf("se esperaba ErrSinCupo, dio %v", err)
	}
	if guardado, _ := actual.repositorio.Buscar(context.Background(), cotizado.ID); guardado.Estado != dominio.EstadoCotizado {
		prueba.Errorf("sin cupo el especial sigue cotizado; quedó %s", guardado.Estado)
	}
}

func TestCotizacionVenceA48Horas(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, fechaEspecial, 5)
	cotizado := actual.especialCotizado(prueba)
	*actual.reloj = actual.reloj.Add(dominio.VigenciaCotizacion + time.Minute)

	_, err := actual.servicio.ResponderEspecial(context.Background(), comprador.ID, cotizado.ID, true)

	if !errors.Is(err, dominio.ErrCotizacionVencida) {
		prueba.Fatalf("se esperaba ErrCotizacionVencida, dio %v", err)
	}
	if guardado, _ := actual.repositorio.Buscar(context.Background(), cotizado.ID); guardado.Estado != dominio.EstadoVencido {
		prueba.Errorf("estado = %s, se esperaba vencido", guardado.Estado)
	}
	if reservados := actual.repositorio.reservados(fechaEspecial); reservados != 0 {
		prueba.Errorf("una cotización vencida no reserva cupo; reservados = %d", reservados)
	}
}

// Si la fecha del pedido ya pasó cuando el comprador responde, la cotización
// vence aunque no hayan pasado 48 h, y no reserva cupo de un día que ya fue.
func TestCotizacionVenceSiYaPasoLaFecha(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, fechaEspecial, 5)
	especial, _ := actual.servicio.SolicitarEspecial(context.Background(), comprador, "clave-especial", "Torta de casamiento", fechaEspecial)
	*actual.reloj = time.Date(2026, 11, 5, 20, 0, 0, 0, time.UTC) // el mismo día, a la tarde
	if _, err := actual.servicio.CotizarEspecial(context.Background(), especial.ID, 9000000); err != nil {
		prueba.Fatalf("cotizar el mismo día: %v", err)
	}
	*actual.reloj = actual.reloj.Add(24 * time.Hour) // al día siguiente: menos de 48 h

	_, err := actual.servicio.ResponderEspecial(context.Background(), comprador.ID, especial.ID, true)

	if !errors.Is(err, dominio.ErrCotizacionVencida) {
		prueba.Fatalf("se esperaba ErrCotizacionVencida, dio %v", err)
	}
	if guardado, _ := actual.repositorio.Buscar(context.Background(), especial.ID); guardado.Estado != dominio.EstadoVencido {
		prueba.Errorf("estado = %s, se esperaba vencido", guardado.Estado)
	}
	if reservados := actual.repositorio.reservados(fechaEspecial); reservados != 0 {
		prueba.Errorf("no se reserva cupo de un día que ya pasó; reservados = %d", reservados)
	}
}

func TestNoSeCotizaUnaFechaPasada(prueba *testing.T) {
	actual := nuevoEntorno()
	especial, _ := actual.servicio.SolicitarEspecial(context.Background(), comprador, "clave-especial", "Torta de casamiento", fechaEspecial)
	*actual.reloj = time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC)

	_, err := actual.servicio.CotizarEspecial(context.Background(), especial.ID, 9000000)

	if !errors.Is(err, dominio.ErrTransicionInvalida) {
		prueba.Fatalf("se esperaba ErrTransicionInvalida, dio %v", err)
	}
	if guardado, _ := actual.repositorio.Buscar(context.Background(), especial.ID); guardado.Estado != dominio.EstadoSolicitado {
		prueba.Errorf("estado = %s, se esperaba solicitado", guardado.Estado)
	}
}

// El comprador puede retirar un especial que todavía no se cotizó. No tiene cupo
// ni insumos reservados, así que no se devuelve nada.
func TestCompradorCancelaUnEspecialSolicitado(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, fechaEspecial, 5)
	contexto := context.Background()
	especial, _ := actual.servicio.SolicitarEspecial(contexto, comprador, "clave-especial", "Torta de casamiento", fechaEspecial)

	if _, err := actual.servicio.CambiarEstado(contexto, especial.ID, dominio.EstadoCancelado); !errors.Is(err, dominio.ErrTransicionInvalida) {
		prueba.Errorf("el vendedor no cancela una solicitud, dio %v", err)
	}
	cancelado, err := actual.servicio.Cancelar(contexto, comprador.ID, especial.ID)

	if err != nil || cancelado.Estado != dominio.EstadoCancelado || cancelado.LiberarInsumos {
		prueba.Fatalf("estado %s, liberar insumos %v, err %v", cancelado.Estado, cancelado.LiberarInsumos, err)
	}
	if reservados := actual.repositorio.reservados(fechaEspecial); reservados != 0 {
		prueba.Errorf("no tenía cupo reservado; reservados = %d", reservados)
	}
	if len(actual.inventario.liberados) != 0 {
		prueba.Errorf("no tenía insumos reservados; se liberaron %v", actual.inventario.liberados)
	}
	if abiertos, _ := actual.servicio.ListarEspecialesAbiertos(contexto); len(abiertos) != 0 {
		prueba.Errorf("un cancelado no queda abierto; hay %d", len(abiertos))
	}
	if estados := actual.repositorio.estadosDeLosEventos(); !slices.Equal(estados, []string{"solicitado", "cancelado"}) {
		prueba.Errorf("eventos = %v", estados)
	}
}

// El vendedor puede no cotizar una solicitud y explicarle al comprador por qué:
// el mensaje viaja en el evento para el mail.
func TestVendedorRechazaUnEspecialSolicitado(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, fechaEspecial, 5)
	contexto := context.Background()
	especial, _ := actual.servicio.SolicitarEspecial(contexto, comprador, "clave-especial", "Torta de seis pisos", fechaEspecial)

	rechazado, err := actual.servicio.RechazarEspecial(contexto, especial.ID, "  Para esa fecha no llegamos con una torta tan grande.  ")

	if err != nil || rechazado.Estado != dominio.EstadoCancelado || rechazado.MensajeVendedor != "Para esa fecha no llegamos con una torta tan grande." {
		prueba.Fatalf("estado %s, mensaje %q, err %v", rechazado.Estado, rechazado.MensajeVendedor, err)
	}
	if reservados := actual.repositorio.reservados(fechaEspecial); reservados != 0 || len(actual.inventario.liberados) != 0 {
		prueba.Errorf("no tenía cupo ni insumos reservados; reservados %d, liberados %v", reservados, actual.inventario.liberados)
	}
	ultimo := actual.repositorio.eventos[len(actual.repositorio.eventos)-1].Evento.Datos
	if ultimo.EstadoNuevo != "cancelado" || ultimo.MensajeVendedor != rechazado.MensajeVendedor {
		prueba.Errorf("evento %+v", ultimo)
	}
	if abiertos, _ := actual.servicio.ListarEspecialesAbiertos(contexto); len(abiertos) != 0 {
		prueba.Errorf("un rechazado por el vendedor no queda abierto; hay %d", len(abiertos))
	}
}

func TestReglasDelRechazoDelVendedor(prueba *testing.T) {
	actual := nuevoEntorno()
	contexto := context.Background()
	especial, _ := actual.servicio.SolicitarEspecial(contexto, comprador, "clave-solicitud", "Torta", fechaEspecial)

	for _, invalido := range []string{"", "   ", strings.Repeat("a", 501)} {
		if _, err := actual.servicio.RechazarEspecial(contexto, especial.ID, invalido); !errors.Is(err, dominio.ErrSolicitudInvalida) {
			prueba.Errorf("mensaje de %d caracteres: se esperaba ErrSolicitudInvalida, dio %v", len(invalido), err)
		}
	}
	cotizado := actual.especialCotizado(prueba)
	if _, err := actual.servicio.RechazarEspecial(contexto, cotizado.ID, "Ya no podemos"); !errors.Is(err, dominio.ErrTransicionInvalida) {
		prueba.Errorf("un cotizado ya tiene precio: se esperaba ErrTransicionInvalida, dio %v", err)
	}
}

// Una cotización se rechaza, no se cancela.
func TestUnEspecialCotizadoNoSeCancela(prueba *testing.T) {
	actual := nuevoEntorno()
	cotizado := actual.especialCotizado(prueba)

	if _, err := actual.servicio.Cancelar(context.Background(), comprador.ID, cotizado.ID); !errors.Is(err, dominio.ErrTransicionInvalida) {
		prueba.Errorf("se esperaba ErrTransicionInvalida, dio %v", err)
	}
}

// Igual que al crear: si se acepta pero falla guardar la confirmación, se
// responde lo que está en la base y lo retoma el proceso en segundo plano.
func TestGuardadoFallidoAlAceptarDevuelveLoQueEstaEnLaBase(prueba *testing.T) {
	actual := nuevoEntorno()
	actual.capacidad(prueba, fechaEspecial, 5)
	cotizado := actual.especialCotizado(prueba)
	actual.inventario.alReservar = func() { actual.repositorio.fallarGuardar = errCaido }

	aceptado, err := actual.servicio.ResponderEspecial(context.Background(), comprador.ID, cotizado.ID, true)

	if err != nil {
		prueba.Fatalf("la aceptación ya está guardada, no tiene que fallar: %v", err)
	}
	guardado, _ := actual.repositorio.Buscar(context.Background(), cotizado.ID)
	if aceptado.Estado != guardado.Estado || guardado.Estado != dominio.EstadoPendienteConfirmacion {
		prueba.Errorf("respondió %s y en la base está %s", aceptado.Estado, guardado.Estado)
	}
	if len(actual.eventos.noGuardadas) != 1 {
		prueba.Errorf("el error de guardado se avisó %d veces, se esperaba 1", len(actual.eventos.noGuardadas))
	}
}

func TestEspecialRechazadoYReglasDeCotizacion(prueba *testing.T) {
	actual := nuevoEntorno()
	contexto := context.Background()
	cotizado := actual.especialCotizado(prueba)

	if _, err := actual.servicio.CotizarEspecial(contexto, cotizado.ID, 100); !errors.Is(err, dominio.ErrTransicionInvalida) {
		prueba.Errorf("cotizar dos veces tenía que dar ErrTransicionInvalida, dio %v", err)
	}
	if _, err := actual.servicio.ResponderEspecial(contexto, "otro-comprador", cotizado.ID, true); !errors.Is(err, dominio.ErrNoEncontrado) {
		prueba.Errorf("otro comprador no ve el especial; dio %v", err)
	}
	rechazado, err := actual.servicio.ResponderEspecial(contexto, comprador.ID, cotizado.ID, false)
	if err != nil || rechazado.Estado != dominio.EstadoRechazado {
		prueba.Errorf("estado %s, err %v", rechazado.Estado, err)
	}
	if abiertos, _ := actual.servicio.ListarEspecialesAbiertos(contexto); len(abiertos) != 0 {
		prueba.Errorf("un rechazado no queda abierto; hay %d", len(abiertos))
	}
}

func TestSolicitarEspecialValidaEsIdempotente(prueba *testing.T) {
	actual := nuevoEntorno()
	contexto := context.Background()

	primero, err := actual.servicio.SolicitarEspecial(contexto, comprador, "clave-1", "Torta", fechaEspecial)
	if err != nil {
		prueba.Fatalf("solicitar: %v", err)
	}
	if repetido, _ := actual.servicio.SolicitarEspecial(contexto, comprador, "clave-1", "Torta", fechaEspecial); repetido.ID != primero.ID {
		prueba.Errorf("la misma clave devuelve la misma solicitud: %s y %s", primero.ID, repetido.ID)
	}
	if _, err := actual.servicio.SolicitarEspecial(contexto, comprador, "clave-1", "Otra cosa", fechaEspecial); !errors.Is(err, dominio.ErrConflictoIdempotencia) {
		prueba.Errorf("la misma clave con otro contenido tenía que dar conflicto, dio %v", err)
	}
	for _, invalida := range []struct{ descripcion, fecha string }{{"", fechaEspecial}, {"Torta", "2026-10-01"}, {"Torta", "mañana"}} {
		if _, err := actual.servicio.SolicitarEspecial(contexto, comprador, "clave-"+invalida.fecha+invalida.descripcion, invalida.descripcion, invalida.fecha); !errors.Is(err, dominio.ErrSolicitudInvalida) {
			prueba.Errorf("%+v tenía que ser inválida, dio %v", invalida, err)
		}
	}
}
