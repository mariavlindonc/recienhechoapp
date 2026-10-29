package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// Tests del chequeo de salud del balanceador (ADR-012).

var errChequeo = errors.New("connection refused")

func turnosDe(router http.Handler, cantidad int) string {
	var turnos []string
	for range cantidad {
		turnos = append(turnos, pedir(router, http.MethodGet, "/notificaciones/x", "").Header().Get("X-Instancia"))
	}
	return strings.Join(turnos, ",")
}

func routerConBalanceador(balanceador *proxyBalanceado) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return nuevoRouter([]byte(secretoDePrueba), balanceador, http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler())
}

func TestDosChequeosFallidosSacanLaInstanciaYUnoOKLaVuelveASumar(prueba *testing.T) {
	uno, _ := instanciaFalsa(prueba, "uno", 0)
	dos, _ := instanciaFalsa(prueba, "dos", 0)
	balanceador := nuevoProxyBalanceado("notificaciones", []*url.URL{uno, dos}, time.Second, nil)
	router := routerConBalanceador(balanceador)

	balanceador.registrarChequeo(0, errChequeo)
	if turnos := turnosDe(router, 2); turnos != "uno,dos" {
		prueba.Fatalf("con una sola falla tenía que seguir en el turno, turnos = %s", turnos)
	}
	balanceador.registrarChequeo(0, errChequeo)
	if turnos := turnosDe(router, 3); turnos != "dos,dos,dos" {
		prueba.Fatalf("con dos fallas tenía que salir del turno, turnos = %s", turnos)
	}
	balanceador.registrarChequeo(0, nil)
	if turnos := turnosDe(router, 2); !strings.Contains(turnos, "uno") {
		prueba.Errorf("con un chequeo OK tenía que volver al turno, turnos = %s", turnos)
	}
}

func TestUnaFallaDeConexionSacaLaInstanciaSinEsperarAlChequeo(prueba *testing.T) {
	servidorCaido := httptest.NewServer(nil)
	caida, _ := url.Parse(servidorCaido.URL)
	servidorCaido.Close()
	sana, _ := instanciaFalsa(prueba, "sana", 0)
	router := routerDePrueba([]*url.URL{caida, sana}, time.Second)

	comprobarNoDisponible(prueba, pedir(router, http.MethodGet, "/notificaciones/x", ""))

	if turnos := turnosDe(router, 3); turnos != "sana,sana,sana" {
		prueba.Errorf("después de la falla todo tenía que ir a la sana, turnos = %s", turnos)
	}
}

func TestSiCortaElClienteLaInstanciaSigueEnElBalanceo(prueba *testing.T) {
	lenta, _ := instanciaFalsa(prueba, "lenta", 2*time.Second)
	otra, _ := instanciaFalsa(prueba, "otra", 0)
	balanceador := nuevoProxyBalanceado("notificaciones", []*url.URL{lenta, otra}, time.Second, nil)
	contexto, cortar := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cortar()
	pedido := httptest.NewRequest(http.MethodGet, "/notificaciones/x", nil).WithContext(contexto)
	pedido.Header.Set("X-API-Key", "clave-a")

	routerConBalanceador(balanceador).ServeHTTP(httptest.NewRecorder(), pedido)

	balanceador.mutex.Lock()
	defer balanceador.mutex.Unlock()
	if !balanceador.sanas[0] {
		prueba.Error("la instancia salió del balanceo, pero el que cortó fue el cliente")
	}
}

func TestSiVenceElTiempoDelGatewayLaInstanciaSale(prueba *testing.T) {
	lenta, _ := instanciaFalsa(prueba, "lenta", 2*time.Second)
	otra, _ := instanciaFalsa(prueba, "otra", 0)
	balanceador := nuevoProxyBalanceado("notificaciones", []*url.URL{lenta, otra}, 50*time.Millisecond, nil)

	comprobarNoDisponible(prueba, pedir(routerConBalanceador(balanceador), http.MethodGet, "/notificaciones/x", ""))

	balanceador.mutex.Lock()
	defer balanceador.mutex.Unlock()
	if balanceador.sanas[0] {
		prueba.Error("la instancia que no respondió a tiempo tenía que salir del balanceo")
	}
}

func TestLaMetricaInicialNoPisaUnaFallaAnterior(prueba *testing.T) {
	uno, _ := instanciaFalsa(prueba, "uno", 0)
	dos, _ := instanciaFalsa(prueba, "dos", 0)
	balanceador := nuevoProxyBalanceado("notificaciones", []*url.URL{uno, dos}, time.Second, nil)
	var mutex sync.Mutex
	ultimo := map[string]bool{}
	balanceador.alCambiarSalud = func(instancia string, sana bool) {
		mutex.Lock()
		defer mutex.Unlock()
		ultimo[instancia] = sana
	}
	balanceador.sacarPorFalla(0, errChequeo)

	contexto, apagar := context.WithCancel(context.Background())
	apagar()
	balanceador.vigilar(contexto, time.Hour, time.Second)

	mutex.Lock()
	defer mutex.Unlock()
	if ultimo[uno.Host] || !ultimo[dos.Host] {
		prueba.Errorf("valores publicados = %v, se esperaba uno caída y dos sana", ultimo)
	}
}

func TestConUnaSolaInstanciaUnaFallaNoLaSaca(prueba *testing.T) {
	lenta, _ := instanciaFalsa(prueba, "lenta", 200*time.Millisecond)
	balanceador := nuevoProxyBalanceado("pedidos", []*url.URL{lenta}, 50*time.Millisecond, nil)
	router := routerConBalanceador(balanceador)

	pedir(router, http.MethodGet, "/notificaciones/x", "")

	if _, hay := balanceador.elegir(); !hay {
		prueba.Error("la única instancia no tenía que salir del turno: no hay chequeo que la devuelva")
	}
}

func TestSinInstanciasSanasRespondeNoDisponible(prueba *testing.T) {
	uno, recibidasUno := instanciaFalsa(prueba, "uno", 0)
	dos, recibidasDos := instanciaFalsa(prueba, "dos", 0)
	balanceador := nuevoProxyBalanceado("notificaciones", []*url.URL{uno, dos}, time.Second, nil)
	for range fallasParaSacar {
		balanceador.registrarChequeo(0, errChequeo)
		balanceador.registrarChequeo(1, errChequeo)
	}

	comprobarNoDisponible(prueba, pedir(routerConBalanceador(balanceador), http.MethodGet, "/notificaciones/x", ""))

	if len(*recibidasUno)+len(*recibidasDos) != 0 {
		prueba.Error("sin instancias sanas no tenía que reenviar nada")
	}
}

func TestVigilarConsultaVivoYAvisaLosCambios(prueba *testing.T) {
	var caida atomic.Bool
	servidor := httptest.NewServer(http.HandlerFunc(func(escritor http.ResponseWriter, pedido *http.Request) {
		if pedido.URL.Path != "/vivo" || caida.Load() {
			escritor.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		escritor.WriteHeader(http.StatusOK)
	}))
	defer servidor.Close()
	direccion, _ := url.Parse(servidor.URL)

	var mutex sync.Mutex
	var cambios []bool
	balanceador := nuevoProxyBalanceado("notificaciones", []*url.URL{direccion}, time.Second, nil)
	balanceador.alCambiarSalud = func(_ string, sana bool) {
		mutex.Lock()
		defer mutex.Unlock()
		cambios = append(cambios, sana)
	}
	contexto, apagar := context.WithCancel(context.Background())
	terminado := make(chan struct{})
	go func() {
		balanceador.vigilar(contexto, 10*time.Millisecond, time.Second)
		close(terminado)
	}()

	esperarHasta := func(descripcion string, condicion func() bool) {
		prueba.Helper()
		limite := time.Now().Add(2 * time.Second)
		for !condicion() {
			if time.Now().After(limite) {
				prueba.Fatalf("no pasó: %s", descripcion)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	estaSana := func() bool { _, hay := balanceador.elegir(); return hay }

	caida.Store(true)
	esperarHasta("salir del balanceo con /vivo caído", func() bool { return !estaSana() })
	caida.Store(false)
	esperarHasta("volver al balanceo con /vivo OK", estaSana)

	apagar()
	select {
	case <-terminado:
	case <-time.After(time.Second):
		prueba.Fatal("vigilar no terminó al cancelar el contexto")
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(cambios) != 3 || !cambios[0] || cambios[1] || !cambios[2] {
		prueba.Errorf("cambios = %v, se esperaba [sana inicial, caída, sana]", cambios)
	}
}
