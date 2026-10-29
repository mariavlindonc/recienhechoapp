package puertos_test

import (
	"slices"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/puertos"
)

func TestResolverTomaLaUltimaOperacionDeCadaProducto(prueba *testing.T) {
	cambios := puertos.CambiosDeIndice{
		{ID: 1, ProductoID: "torta", Operacion: puertos.IndiceGuardar},
		{ID: 2, ProductoID: "tarta", Operacion: puertos.IndiceGuardar},
		{ID: 3, ProductoID: "torta", Operacion: puertos.IndiceBorrar}, // se modificó y después se dio de baja
		{ID: 4, ProductoID: "pan", Operacion: puertos.IndiceBorrar},
		{ID: 5, ProductoID: "pan", Operacion: puertos.IndiceGuardar},   // se dio de baja y se volvió a cargar
		{ID: 6, ProductoID: "tarta", Operacion: puertos.IndiceGuardar}, // dos modificaciones: se guarda una vez
	}

	guardar, borrar := cambios.Resolver()

	if !slices.Equal(guardar, []string{"tarta", "pan"}) || !slices.Equal(borrar, []string{"torta"}) {
		prueba.Errorf("guardar %v, borrar %v", guardar, borrar)
	}
	if !slices.Equal(cambios.IDs(), []int64{1, 2, 3, 4, 5, 6}) {
		prueba.Errorf("ids = %v", cambios.IDs())
	}
}

func TestAntiguedadEsLaDelCambioMasViejo(prueba *testing.T) {
	ahora := time.Date(2026, 10, 26, 15, 0, 10, 0, time.UTC)
	cambios := puertos.CambiosDeIndice{{CreadoEn: ahora.Add(-7 * time.Second)}, {CreadoEn: ahora.Add(-time.Second)}}

	if antiguedad := cambios.Antiguedad(ahora); antiguedad != 7*time.Second {
		prueba.Errorf("antigüedad = %v", antiguedad)
	}
	if antiguedad := (puertos.CambiosDeIndice{}).Antiguedad(ahora); antiguedad != 0 {
		prueba.Errorf("sin cambios la antigüedad es 0, dio %v", antiguedad)
	}
}
