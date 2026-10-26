package main

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/adaptadores/api"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/adaptadores/smtp"
)

// configuracion reúne todo lo que el servicio lee de las variables de entorno.
type configuracion struct {
	puerto           string
	puertoMetricas   string
	direccionMongo   string
	direccionRabbit  string
	baseMongo        string
	clavesAPI        map[string]string
	limitePorMinuto  int
	intervaloDeEnvio time.Duration
	smtp             smtp.Configuracion
}

// leerConfiguracion recibe la función que busca cada variable (os.Getenv en el
// servicio, un mapa en los tests). Junta todos los problemas en un solo error para
// que se puedan corregir de una vez.
func leerConfiguracion(buscar func(nombre string) string) (configuracion, error) {
	var problemas []error
	obligatoria := func(nombre string) string {
		valor := buscar(nombre)
		if valor == "" {
			problemas = append(problemas, fmt.Errorf("falta la variable %s", nombre))
		}
		return valor
	}
	conValorPorDefecto := func(nombre, porDefecto string) string {
		if valor := buscar(nombre); valor != "" {
			return valor
		}
		return porDefecto
	}
	entero := func(nombre, porDefecto string) int {
		valor, err := strconv.Atoi(conValorPorDefecto(nombre, porDefecto))
		if err != nil || valor <= 0 {
			problemas = append(problemas, fmt.Errorf("%s tiene que ser un entero positivo", nombre))
		}
		return valor
	}
	duracion := func(nombre, porDefecto string) time.Duration {
		valor, err := time.ParseDuration(conValorPorDefecto(nombre, porDefecto))
		if err != nil || valor <= 0 {
			problemas = append(problemas, fmt.Errorf("%s tiene que ser una duración positiva, como 5s", nombre))
		}
		return valor
	}

	leida := configuracion{
		puerto:         conValorPorDefecto("PORT", "8084"),
		puertoMetricas: conValorPorDefecto("METRICAS_PORT", "9464"),
		direccionMongo: obligatoria("MONGO_URI"),
		baseMongo:      conValorPorDefecto("MONGO_BASE", "notificaciones"),
		// Opcional: la capacidad publicada funciona sola, sin los eventos de pedidos.
		direccionRabbit:  buscar("RABBITMQ_URL"),
		limitePorMinuto:  entero("NOTIFICACIONES_LIMITE_POR_MINUTO", "60"),
		intervaloDeEnvio: duracion("NOTIFICACIONES_INTERVALO_DE_ENVIO", "5s"),
		smtp: smtp.Configuracion{
			Host:                 obligatoria("SMTP_HOST"),
			Puerto:               entero("SMTP_PUERTO", "587"),
			Usuario:              buscar("SMTP_USUARIO"),
			Clave:                buscar("SMTP_CLAVE"),
			Remitente:            obligatoria("SMTP_REMITENTE"),
			TiempoMaximo:         duracion("SMTP_TIEMPO_MAXIMO", "10s"),
			TiempoMaximoConexion: duracion("SMTP_TIEMPO_MAXIMO_CONEXION", "2s"),
		},
	}

	if textoClaves := obligatoria("NOTIFICACIONES_CLAVES_API"); textoClaves != "" {
		claves, err := api.ParsearClavesAPI(textoClaves)
		if err != nil {
			problemas = append(problemas, err)
		}
		leida.clavesAPI = claves
	}

	if len(problemas) > 0 {
		return configuracion{}, fmt.Errorf("configuración inválida: %w", errors.Join(problemas...))
	}
	return leida, nil
}
