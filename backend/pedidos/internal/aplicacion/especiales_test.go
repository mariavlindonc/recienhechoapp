package aplicacion_test

import (
	"context"
	"errors"
	"slices"
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
