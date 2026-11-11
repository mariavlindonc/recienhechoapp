// Package smtp implementa el puerto Remitente enviando emails por SMTP (ADR-005).
// Sirve igual para Mailpit (desarrollo) y Gmail (demo): solo cambia la configuración.
package smtp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

// Configuracion del servidor de correo. Sale de las variables SMTP_* del entorno.
type Configuracion struct {
	Host    string
	Puerto  int
	Usuario string // vacío si el servidor no pide autenticación (Mailpit)
	Clave   string
	// Remitente puede llevar nombre: "Recién hecho <avisos@ejemplo.com>".
	Remitente string
	// TiempoMaximo es cuánto puede tardar un envío completo antes de cortarlo.
	TiempoMaximo time.Duration
	// TiempoMaximoConexion es cuánto se espera para conectar con el servidor. Es
	// más corto que TiempoMaximo para que un servidor caído falle rápido (ADR-011).
	TiempoMaximoConexion time.Duration
}

// Remitente cumple puertos.Remitente.
type Remitente struct {
	configuracion Configuracion
	remitente     *mail.Address
}

var _ puertos.Remitente = (*Remitente)(nil)

// NuevoRemitente valida la configuración. No se conecta todavía: si el servidor de
// correo está caído al arrancar, el servicio igual levanta y los envíos se reintentan.
func NuevoRemitente(configuracion Configuracion) (*Remitente, error) {
	if configuracion.Host == "" || configuracion.Puerto <= 0 {
		return nil, fmt.Errorf("configuración SMTP: faltan el host o el puerto")
	}
	if configuracion.TiempoMaximo <= 0 || configuracion.TiempoMaximoConexion <= 0 {
		return nil, fmt.Errorf("configuración SMTP: los tiempos máximos tienen que ser positivos")
	}
	remitente, err := mail.ParseAddress(configuracion.Remitente)
	if err != nil {
		return nil, fmt.Errorf("configuración SMTP: remitente %q inválido: %w", configuracion.Remitente, err)
	}
	return &Remitente{configuracion: configuracion, remitente: remitente}, nil
}

// Enviar hace el diálogo SMTP completo. Un 5xx al destinatario o al mensaje se
// devuelve como puertos.ErrEnvioRechazado (no se reintenta); cualquier otro error
// se considera transitorio.
func (remitente *Remitente) Enviar(contexto context.Context, mensaje puertos.Mensaje) error {
	contexto, cancelar := context.WithTimeout(contexto, remitente.configuracion.TiempoMaximo)
	defer cancelar()

	direccion := net.JoinHostPort(remitente.configuracion.Host, strconv.Itoa(remitente.configuracion.Puerto))
	marcador := net.Dialer{Timeout: remitente.configuracion.TiempoMaximoConexion}
	conexion, err := marcador.DialContext(contexto, "tcp", direccion)
	if err != nil {
		return fmt.Errorf("conectar con el servidor de correo: %w", err)
	}
	// El paquete net/smtp no usa contextos: el límite se aplica sobre la conexión.
	if limite, hayLimite := contexto.Deadline(); hayLimite {
		conexion.SetDeadline(limite)
	}

	cliente, err := smtp.NewClient(conexion, remitente.configuracion.Host)
	if err != nil {
		conexion.Close()
		return fmt.Errorf("iniciar el diálogo SMTP: %w", err)
	}
	defer cliente.Close()

	// Cifra la conexión si el servidor lo ofrece (Gmail sí, Mailpit no), antes de
	// mandar la contraseña.
	if ofreceTLS, _ := cliente.Extension("STARTTLS"); ofreceTLS {
		if err := cliente.StartTLS(&tls.Config{ServerName: remitente.configuracion.Host}); err != nil {
			return fmt.Errorf("cifrar la conexión SMTP: %w", err)
		}
	}
	// PlainAuth se niega a mandar la clave sin cifrar, salvo a localhost.
	if remitente.configuracion.Usuario != "" {
		autenticacion := smtp.PlainAuth("", remitente.configuracion.Usuario, remitente.configuracion.Clave, remitente.configuracion.Host)
		if err := cliente.Auth(autenticacion); err != nil {
			return fmt.Errorf("autenticarse en el servidor de correo: %w", err)
		}
	}

	if err := cliente.Mail(remitente.remitente.Address); err != nil {
		return clasificar("indicar el remitente", err, false)
	}
	if err := cliente.Rcpt(mensaje.Destinatario); err != nil {
		return clasificar("indicar el destinatario", err, true)
	}
	escritor, err := cliente.Data()
	if err != nil {
		return clasificar("empezar el mensaje", err, false)
	}
	if _, err := escritor.Write(remitente.armarMensaje(mensaje, time.Now())); err != nil {
		return fmt.Errorf("escribir el mensaje: %w", err)
	}
	if err := escritor.Close(); err != nil {
		return clasificar("entregar el mensaje", err, true)
	}

	// El servidor ya aceptó el mensaje: si falla la despedida, el mail igual salió.
	cliente.Quit()
	return nil
}

// clasificar decide si un error del servidor es un rechazo definitivo. Los
// códigos 5xx son definitivos y los 4xx temporales. Solo se toma como rechazo en
// los pasos que dependen del mensaje (destinatario, contenido); un 5xx en otro
// paso suele ser un problema de configuración nuestra y se reintenta.
func clasificar(paso string, err error, puedeSerRechazo bool) error {
	var errorDelServidor *textproto.Error
	if puedeSerRechazo && errors.As(err, &errorDelServidor) && errorDelServidor.Code >= 500 {
		return fmt.Errorf("%w: %s (%d %s)", puertos.ErrEnvioRechazado, paso, errorDelServidor.Code, errorDelServidor.Msg)
	}
	return fmt.Errorf("%s: %w", paso, err)
}

// armarMensaje arma el email en formato estándar (encabezados + cuerpo), con el
// asunto y el cuerpo codificados para que lleguen bien las tildes y las eñes.
func (remitente *Remitente) armarMensaje(mensaje puertos.Mensaje, fecha time.Time) []byte {
	// Un salto de línea en el asunto permitiría agregar encabezados (por ejemplo,
	// una copia oculta a otra persona): se reemplazan por espacios.
	asunto := strings.NewReplacer("\r", " ", "\n", " ").Replace(mensaje.Asunto)

	var contenido bytes.Buffer
	encabezados := []string{
		"From: " + remitente.remitente.String(),
		"To: " + mensaje.Destinatario,
		"Subject: " + mime.QEncoding.Encode("utf-8", asunto),
		"Date: " + fecha.Format(time.RFC1123Z),
		"Message-ID: " + remitente.nuevoIDDeMensaje(),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: quoted-printable",
	}
	contenido.WriteString(strings.Join(encabezados, "\r\n"))
	contenido.WriteString("\r\n\r\n")

	codificador := quotedprintable.NewWriter(&contenido)
	codificador.Write([]byte(mensaje.Cuerpo))
	codificador.Close()
	return contenido.Bytes()
}

// nuevoIDDeMensaje genera un Message-ID único; sin él, algunos servidores marcan
// el mail como spam.
func (remitente *Remitente) nuevoIDDeMensaje() string {
	aleatorio := make([]byte, 16)
	rand.Read(aleatorio)
	dominio := remitente.remitente.Address[strings.LastIndex(remitente.remitente.Address, "@")+1:]
	return "<" + hex.EncodeToString(aleatorio) + "@" + dominio + ">"
}
