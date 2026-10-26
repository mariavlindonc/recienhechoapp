package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mariavlindonc/recienhechoapp/backend/productos/internal/adaptadores/api"
)

func pedirSalud(chequear func(context.Context) error, ruta string) int {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api.Registrar(router, chequear)
	respuesta := httptest.NewRecorder()
	router.ServeHTTP(respuesta, httptest.NewRequest(http.MethodGet, ruta, nil))
	return respuesta.Code
}

func TestSaludDependeDeLasDependencias(prueba *testing.T) {
	if estado := pedirSalud(func(context.Context) error { return nil }, "/health"); estado != http.StatusOK {
		prueba.Errorf("con MySQL sano se esperaba 200, dio %d", estado)
	}
	if estado := pedirSalud(func(context.Context) error { return errors.New("MySQL caído") }, "/health"); estado != http.StatusServiceUnavailable {
		prueba.Errorf("con MySQL caído se esperaba 503, dio %d", estado)
	}
}

func TestVivoNoMiraLasDependencias(prueba *testing.T) {
	if estado := pedirSalud(func(context.Context) error { return errors.New("MySQL caído") }, "/vivo"); estado != http.StatusOK {
		prueba.Errorf("/vivo no mira dependencias, dio %d", estado)
	}
}
