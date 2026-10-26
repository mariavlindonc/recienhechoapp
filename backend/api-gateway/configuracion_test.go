package main

import (
	"strings"
	"testing"
)

func TestLeerConfiguracionUsaValoresPorDefecto(prueba *testing.T) {
	variables := map[string]string{"NOTIFICACIONES_URLS": "http://notificaciones:8084", "PEDIDOS_URLS": "http://pedidos:8083"}

	leida, err := leerConfiguracion(func(nombre string) string { return variables[nombre] })

	if err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}
	if leida.puerto != "8080" || leida.puertoMetricas != "9464" {
		prueba.Errorf("valores por defecto incorrectos: %+v", leida)
	}
	if len(leida.instanciasNotificaciones) != 1 || leida.instanciasNotificaciones[0].Host != "notificaciones:8084" {
		prueba.Errorf("instancias = %v", leida.instanciasNotificaciones)
	}
	if len(leida.instanciasPedidos) != 1 || leida.instanciasPedidos[0].Host != "pedidos:8083" {
		prueba.Errorf("instancias de pedidos = %v", leida.instanciasPedidos)
	}
}

func TestLeerConfiguracionInformaLosProblemas(prueba *testing.T) {
	casos := map[string]map[string]string{
		"falta NOTIFICACIONES_URLS":    {},
		"NOTIFICACIONES_URLS inválida": {"NOTIFICACIONES_URLS": "notificaciones:8084", "PEDIDOS_URLS": "http://pedidos:8083"},
	}

	for nombre, variables := range casos {
		prueba.Run(nombre, func(prueba *testing.T) {
			_, err := leerConfiguracion(func(nombre string) string { return variables[nombre] })

			if err == nil || !strings.Contains(err.Error(), "NOTIFICACIONES_URLS") {
				prueba.Errorf("se esperaba un error que mencione NOTIFICACIONES_URLS, dio: %v", err)
			}
		})
	}
}

func TestParsearInstancias(prueba *testing.T) {
	instancias, err := parsearInstancias(" http://notificaciones-1:8084 , http://notificaciones-2:8084,")
	if err != nil || len(instancias) != 2 || instancias[1].Host != "notificaciones-2:8084" {
		prueba.Fatalf("instancias = %v, err = %v", instancias, err)
	}

	for _, invalido := range []string{"", " , ", "notificaciones:8084", "ftp://notificaciones:8084", "http://"} {
		if _, err := parsearInstancias(invalido); err == nil {
			prueba.Errorf("%q tenía que dar error", invalido)
		}
	}
}

func TestLeerConfiguracionExigePedidos(prueba *testing.T) {
	variables := map[string]string{"NOTIFICACIONES_URLS": "http://notificaciones:8084", "PEDIDOS_URLS": "pedidos"}

	_, err := leerConfiguracion(func(nombre string) string { return variables[nombre] })

	if err == nil || !strings.Contains(err.Error(), "PEDIDOS_URLS") {
		prueba.Errorf("se esperaba un error que mencione PEDIDOS_URLS, dio: %v", err)
	}
}
