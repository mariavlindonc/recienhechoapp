package main

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// configuracion reúne todo lo que el gateway lee de las variables de entorno.
type configuracion struct {
	puerto                   string
	puertoMetricas           string
	instanciasNotificaciones []*url.URL
	instanciasPedidos        []*url.URL
	instanciasProductos      []*url.URL
	instanciasUsuarios       []*url.URL
	// secretoJWT es el mismo con el que usuarios firma los tokens (ADR-016).
	secretoJWT []byte
	// clavesAPI asocia cada clave de notificaciones con su grupo (clave -> grupo),
	// para contar el límite por grupo (ADR-004).
	clavesAPI       map[string]string
	limitePorMinuto int
}

// largoMinimoSecreto es el mínimo de JWT_SECRETO, igual que en usuarios (ADR-016).
const largoMinimoSecreto = 32

// leerConfiguracion recibe la función que busca cada variable (os.Getenv en el
// servicio, un mapa en los tests). Junta todos los problemas en un solo error para
// que se puedan corregir de una vez. Sigue el mismo esquema que notificaciones.
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

	leida := configuracion{
		puerto:         conValorPorDefecto("PORT", "8080"),
		puertoMetricas: conValorPorDefecto("METRICAS_PORT", "9464"),
	}
	limite, err := strconv.Atoi(conValorPorDefecto("NOTIFICACIONES_LIMITE_POR_MINUTO", "60"))
	if err != nil || limite <= 0 {
		problemas = append(problemas, errors.New("NOTIFICACIONES_LIMITE_POR_MINUTO tiene que ser un entero positivo"))
	}
	leida.limitePorMinuto = limite
	if texto := obligatoria("NOTIFICACIONES_CLAVES_API"); texto != "" {
		claves, err := parsearClavesAPI(texto)
		if err != nil {
			problemas = append(problemas, fmt.Errorf("NOTIFICACIONES_CLAVES_API: %w", err))
		}
		leida.clavesAPI = claves
	}
	if texto := obligatoria("NOTIFICACIONES_URLS"); texto != "" {
		instancias, err := parsearInstancias(texto)
		if err != nil {
			problemas = append(problemas, fmt.Errorf("NOTIFICACIONES_URLS: %w", err))
		}
		leida.instanciasNotificaciones = instancias
	}
	if texto := obligatoria("PEDIDOS_URLS"); texto != "" {
		instancias, err := parsearInstancias(texto)
		if err != nil {
			problemas = append(problemas, fmt.Errorf("PEDIDOS_URLS: %w", err))
		}
		leida.instanciasPedidos = instancias
	}
	if texto := obligatoria("PRODUCTOS_URLS"); texto != "" {
		instancias, err := parsearInstancias(texto)
		if err != nil {
			problemas = append(problemas, fmt.Errorf("PRODUCTOS_URLS: %w", err))
		}
		leida.instanciasProductos = instancias
	}
	if texto := obligatoria("USUARIOS_URLS"); texto != "" {
		instancias, err := parsearInstancias(texto)
		if err != nil {
			problemas = append(problemas, fmt.Errorf("USUARIOS_URLS: %w", err))
		}
		leida.instanciasUsuarios = instancias
	}
	if secreto := obligatoria("JWT_SECRETO"); secreto != "" {
		if len(secreto) < largoMinimoSecreto {
			problemas = append(problemas, fmt.Errorf("JWT_SECRETO tiene que tener al menos %d bytes", largoMinimoSecreto))
		}
		leida.secretoJWT = []byte(secreto)
	}

	if len(problemas) > 0 {
		return configuracion{}, fmt.Errorf("configuración inválida: %w", errors.Join(problemas...))
	}
	return leida, nil
}

// parsearInstancias lee una lista de URLs separadas por comas, como la de
// NOTIFICACIONES_URLS: "http://notificaciones-1:8084,http://notificaciones-2:8084".
func parsearInstancias(texto string) ([]*url.URL, error) {
	var instancias []*url.URL
	for _, parte := range strings.Split(texto, ",") {
		parte = strings.TrimSpace(parte)
		if parte == "" {
			continue
		}
		direccion, err := url.Parse(parte)
		if err != nil || (direccion.Scheme != "http" && direccion.Scheme != "https") || direccion.Host == "" {
			return nil, fmt.Errorf("instancia %q inválida: tiene que ser una URL http://host:puerto", parte)
		}
		instancias = append(instancias, direccion)
	}
	if len(instancias) == 0 {
		return nil, fmt.Errorf("no hay ninguna instancia configurada")
	}
	return instancias, nil
}

// parsearClavesAPI lee NOTIFICACIONES_CLAVES_API, con el formato
// "grupo-a:clave1,grupo-b:clave2", y devuelve el mapa clave -> grupo. Copiado de
// notificaciones: las dos lecturas tienen que coincidir.
func parsearClavesAPI(texto string) (map[string]string, error) {
	claves := map[string]string{}
	for _, par := range strings.Split(texto, ",") {
		par = strings.TrimSpace(par)
		if par == "" {
			continue
		}
		grupo, clave, tieneSeparador := strings.Cut(par, ":")
		grupo, clave = strings.TrimSpace(grupo), strings.TrimSpace(clave)
		if !tieneSeparador || grupo == "" || clave == "" {
			return nil, fmt.Errorf("claves de API: %q no tiene el formato grupo:clave", par)
		}
		if _, repetida := claves[clave]; repetida {
			return nil, fmt.Errorf("claves de API: la clave del grupo %q está repetida", grupo)
		}
		claves[clave] = grupo
	}
	if len(claves) == 0 {
		return nil, errors.New("claves de API: no hay ninguna configurada")
	}
	return claves, nil
}
