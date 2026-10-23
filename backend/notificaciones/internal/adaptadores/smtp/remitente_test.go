package smtp_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/adaptadores/smtp"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

// servidorFalso habla lo mínimo de SMTP para probar el remitente sin internet.
type servidorFalso struct {
	escucha       net.Listener
	respuestaRcpt string        // lo que contesta al RCPT TO
	sinSaludo     bool          // acepta la conexión pero nunca contesta
	sobres        chan string   // "remitente -> destinatario" de cada envío
	mensajes      chan string   // contenido recibido en cada DATA
	terminar      chan struct{} // cierra las conexiones colgadas al terminar el test
}

func iniciarServidorFalso(prueba *testing.T, respuestaRcpt string) *servidorFalso {
	prueba.Helper()
	escucha, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		prueba.Fatalf("abrir el servidor falso: %v", err)
	}
	servidor := &servidorFalso{
		escucha:       escucha,
		respuestaRcpt: respuestaRcpt,
		sobres:        make(chan string, 10),
		mensajes:      make(chan string, 10),
		terminar:      make(chan struct{}),
	}
	prueba.Cleanup(func() {
		close(servidor.terminar)
		escucha.Close()
	})
	go servidor.atender()
	return servidor
}

func (servidor *servidorFalso) atender() {
	for {
		conexion, err := servidor.escucha.Accept()
		if err != nil {
			return
		}
		go servidor.conversar(conexion)
	}
}

func (servidor *servidorFalso) conversar(conexion net.Conn) {
	defer conexion.Close()
	if servidor.sinSaludo {
		<-servidor.terminar
		return
	}
	lector := bufio.NewReader(conexion)
	responder := func(linea string) { fmt.Fprintf(conexion, "%s\r\n", linea) }
	var remitente string

	responder("220 servidor falso")
	for {
		linea, err := lector.ReadString('\n')
		if err != nil {
			return
		}
		comando := strings.TrimRight(linea, "\r\n")
		switch mayusculas := strings.ToUpper(comando); {
		case strings.HasPrefix(mayusculas, "EHLO"):
			responder("250-servidor falso")
			responder("250 8BITMIME")
		case strings.HasPrefix(mayusculas, "MAIL FROM:"):
			// Solo la dirección; el cliente puede agregar parámetros como BODY=8BITMIME.
			remitente, _, _ = strings.Cut(comando[len("MAIL FROM:"):], " ")
			responder("250 ok")
		case strings.HasPrefix(mayusculas, "RCPT TO:"):
			if strings.HasPrefix(servidor.respuestaRcpt, "250") {
				servidor.sobres <- remitente + " -> " + comando[len("RCPT TO:"):]
			}
			responder(servidor.respuestaRcpt)
		case mayusculas == "DATA":
			responder("354 adelante")
			var contenido strings.Builder
			for {
				renglon, err := lector.ReadString('\n')
				if err != nil {
					return
				}
				if renglon == ".\r\n" {
					break
				}
				contenido.WriteString(renglon)
			}
			servidor.mensajes <- contenido.String()
			responder("250 recibido")
		case mayusculas == "QUIT":
			responder("221 chau")
			return
		default:
			responder("502 no implementado")
		}
	}
}

func (servidor *servidorFalso) puerto() int {
	return servidor.escucha.Addr().(*net.TCPAddr).Port
}

func nuevoRemitente(prueba *testing.T, puerto int, tiempoMaximo time.Duration) *smtp.Remitente {
	prueba.Helper()
	remitente, err := smtp.NuevoRemitente(smtp.Configuracion{
		Host:                 "127.0.0.1",
		Puerto:               puerto,
		Remitente:            "Recién hecho <avisos@recienhecho.test>",
		TiempoMaximo:         tiempoMaximo,
		TiempoMaximoConexion: tiempoMaximo,
	})
	if err != nil {
		prueba.Fatalf("crear remitente: %v", err)
	}
	return remitente
}

var mensajeDePrueba = puertos.Mensaje{
	Destinatario: "cliente@ejemplo.com",
	Asunto:       "Tu pedido está listo",
	Cuerpo:       "Ya podés retirarlo.",
}

func TestEnviarEntregaElMensajeConTildes(prueba *testing.T) {
	servidor := iniciarServidorFalso(prueba, "250 ok")
	remitente := nuevoRemitente(prueba, servidor.puerto(), 5*time.Second)

	if err := remitente.Enviar(context.Background(), mensajeDePrueba); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}

	if sobre := <-servidor.sobres; sobre != "<avisos@recienhecho.test> -> <cliente@ejemplo.com>" {
		prueba.Errorf("sobre = %q", sobre)
	}
	recibido, err := mail.ReadMessage(strings.NewReader(<-servidor.mensajes))
	if err != nil {
		prueba.Fatalf("el mensaje no tiene formato de email: %v", err)
	}
	asunto, _ := new(mime.WordDecoder).DecodeHeader(recibido.Header.Get("Subject"))
	if asunto != mensajeDePrueba.Asunto {
		prueba.Errorf("asunto = %q, se esperaba %q", asunto, mensajeDePrueba.Asunto)
	}
	if para := recibido.Header.Get("To"); para != mensajeDePrueba.Destinatario {
		prueba.Errorf("To = %q", para)
	}
	cuerpo, _ := io.ReadAll(quotedprintable.NewReader(recibido.Body))
	if strings.TrimRight(string(cuerpo), "\r\n") != mensajeDePrueba.Cuerpo {
		prueba.Errorf("cuerpo = %q, se esperaba %q", cuerpo, mensajeDePrueba.Cuerpo)
	}
	if recibido.Header.Get("Message-ID") == "" {
		prueba.Error("falta el Message-ID")
	}
}

func TestDestinatarioRechazadoEsDefinitivo(prueba *testing.T) {
	servidor := iniciarServidorFalso(prueba, "550 5.1.1 la casilla no existe")
	remitente := nuevoRemitente(prueba, servidor.puerto(), 5*time.Second)

	err := remitente.Enviar(context.Background(), mensajeDePrueba)

	if !errors.Is(err, puertos.ErrEnvioRechazado) {
		prueba.Errorf("un 550 es un rechazo definitivo; dio: %v", err)
	}
}

func TestErrorTemporalDelServidorSeReintenta(prueba *testing.T) {
	servidor := iniciarServidorFalso(prueba, "451 4.3.0 probá más tarde")
	remitente := nuevoRemitente(prueba, servidor.puerto(), 5*time.Second)

	err := remitente.Enviar(context.Background(), mensajeDePrueba)

	if err == nil || errors.Is(err, puertos.ErrEnvioRechazado) {
		prueba.Errorf("un 451 es temporal (se reintenta); dio: %v", err)
	}
}

func TestServidorApagadoSeReintenta(prueba *testing.T) {
	escucha, _ := net.Listen("tcp", "127.0.0.1:0")
	puertoLibre := escucha.Addr().(*net.TCPAddr).Port
	escucha.Close()
	remitente := nuevoRemitente(prueba, puertoLibre, 5*time.Second)

	err := remitente.Enviar(context.Background(), mensajeDePrueba)

	if err == nil || errors.Is(err, puertos.ErrEnvioRechazado) {
		prueba.Errorf("sin servidor el error es temporal (se reintenta); dio: %v", err)
	}
}

func TestServidorQueNoRespondeSeCortaPorTiempo(prueba *testing.T) {
	servidor := iniciarServidorFalso(prueba, "250 ok")
	servidor.sinSaludo = true
	remitente := nuevoRemitente(prueba, servidor.puerto(), 200*time.Millisecond)

	inicio := time.Now()
	err := remitente.Enviar(context.Background(), mensajeDePrueba)

	if err == nil || errors.Is(err, puertos.ErrEnvioRechazado) {
		prueba.Errorf("un servidor colgado es un error temporal; dio: %v", err)
	}
	if demora := time.Since(inicio); demora > 2*time.Second {
		prueba.Errorf("tardó %v en cortar, se esperaba alrededor de 200 ms", demora)
	}
}

func TestAsuntoConSaltoDeLineaNoAgregaEncabezados(prueba *testing.T) {
	servidor := iniciarServidorFalso(prueba, "250 ok")
	remitente := nuevoRemitente(prueba, servidor.puerto(), 5*time.Second)
	malicioso := mensajeDePrueba
	malicioso.Asunto = "Hola\r\nBcc: espia@ejemplo.com"

	if err := remitente.Enviar(context.Background(), malicioso); err != nil {
		prueba.Fatalf("error inesperado: %v", err)
	}

	recibido, _ := mail.ReadMessage(strings.NewReader(<-servidor.mensajes))
	if copia := recibido.Header.Get("Bcc"); copia != "" {
		prueba.Errorf("se inyectó un encabezado Bcc: %q", copia)
	}
}

func TestConfiguracionInvalida(prueba *testing.T) {
	valida := smtp.Configuracion{Host: "localhost", Puerto: 1025, Remitente: "avisos@recienhecho.test", TiempoMaximo: time.Second, TiempoMaximoConexion: time.Second}
	casos := map[string]func(configuracion *smtp.Configuracion){
		"sin host":               func(configuracion *smtp.Configuracion) { configuracion.Host = "" },
		"sin puerto":             func(configuracion *smtp.Configuracion) { configuracion.Puerto = 0 },
		"remitente inválido":     func(configuracion *smtp.Configuracion) { configuracion.Remitente = "no es un mail" },
		"sin tiempo máximo":      func(configuracion *smtp.Configuracion) { configuracion.TiempoMaximo = 0 },
		"sin tiempo de conexión": func(configuracion *smtp.Configuracion) { configuracion.TiempoMaximoConexion = 0 },
	}

	for nombre, cambiar := range casos {
		prueba.Run(nombre, func(prueba *testing.T) {
			configuracion := valida
			cambiar(&configuracion)
			if _, err := smtp.NuevoRemitente(configuracion); err == nil {
				prueba.Error("se esperaba un error de configuración")
			}
		})
	}

	if _, err := smtp.NuevoRemitente(valida); err != nil {
		prueba.Errorf("la configuración válida dio error: %v", err)
	}
}
