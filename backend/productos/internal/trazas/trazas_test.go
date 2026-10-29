package trazas_test

import (
	"context"
	"testing"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/trazas"
)

func TestIniciarSinDestinoNoFalla(prueba *testing.T) {
	prueba.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	apagar, err := trazas.Iniciar(context.Background(), "productos")

	if err != nil {
		prueba.Fatalf("sin destino no tiene que fallar: %v", err)
	}
	if err := apagar(context.Background()); err != nil {
		prueba.Errorf("apagar: %v", err)
	}
}
