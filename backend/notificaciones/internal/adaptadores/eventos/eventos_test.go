package eventos_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/adaptadores/eventos"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

// contadorFalso cumple lo que el adaptador necesita de las métricas.
type contadorFalso struct {
	resultados []puertos.ResultadoEnvio
}

func (contador *contadorFalso) ContarEnvio(resultado puertos.ResultadoEnvio) {
	contador.resultados = append(contador.resultados, resultado)
}

func nuevoAdaptador() (*eventos.Eventos, *contadorFalso, *bytes.Buffer) {
	contador := &contadorFalso{}
	var salida bytes.Buffer
	return eventos.Nuevos(contador, slog.New(slog.NewJSONHandler(&salida, nil))), contador, &salida
}

func TestUnEnvioExitosoSoloSeCuenta(prueba *testing.T) {
	adaptador, contador, salida := nuevoAdaptador()

	adaptador.EnvioIntentado(context.Background(), puertos.IntentoDeEnvio{IDNotificacion: "id-1", Numero: 1, Resultado: puertos.EnvioExitoso})

	if len(contador.resultados) != 1 || contador.resultados[0] != puertos.EnvioExitoso {
		prueba.Errorf("resultados contados = %v", contador.resultados)
	}
	if salida.Len() != 0 {
		prueba.Errorf("un envío exitoso no deja log: %s", salida.String())
	}
}

func TestUnEnvioFallidoSeCuentaYQuedaEnElLog(prueba *testing.T) {
	adaptador, contador, salida := nuevoAdaptador()

	adaptador.EnvioIntentado(context.Background(), puertos.IntentoDeEnvio{
		IDNotificacion: "id-1", Numero: 2, Resultado: puertos.EnvioReintento,
		Error: errors.New("servidor de correo caído"),
	})

	if len(contador.resultados) != 1 || contador.resultados[0] != puertos.EnvioReintento {
		prueba.Errorf("resultados contados = %v", contador.resultados)
	}
	var linea map[string]any
	if err := json.Unmarshal(salida.Bytes(), &linea); err != nil {
		prueba.Fatalf("se esperaba una línea JSON, llegó %q (%v)", salida.String(), err)
	}
	esperado := map[string]any{"level": "WARN", "id": "id-1", "intento": float64(2), "resultado": "reintento", "error": "servidor de correo caído"}
	for campo, valor := range esperado {
		if linea[campo] != valor {
			prueba.Errorf("%s = %v, se esperaba %v", campo, linea[campo], valor)
		}
	}
}

func TestElLogTapaLosEmailsDelError(prueba *testing.T) {
	adaptador, _, salida := nuevoAdaptador()

	adaptador.EnvioIntentado(context.Background(), puertos.IntentoDeEnvio{
		IDNotificacion: "id-1", Numero: 1, Resultado: puertos.EnvioFallido,
		Error: errors.New("indicar el destinatario (550 5.1.1 <cliente@ejemplo.com>: la casilla no existe)"),
	})

	if strings.Contains(salida.String(), "cliente@ejemplo.com") {
		prueba.Errorf("el log incluye la dirección: %s", salida.String())
	}
	if !strings.Contains(salida.String(), "550 5.1.1 <[email oculto]>: la casilla no existe") {
		prueba.Errorf("el resto del error se tiene que mantener: %s", salida.String())
	}
}
