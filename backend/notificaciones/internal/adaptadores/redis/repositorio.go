// Package redis implementa el puerto Repositorio sobre Redis, para correr
// notificaciones en el Key Value gratis de Render (ADR-013). Ese Key Value no
// persiste: si se reinicia, se pierden las notificaciones y las claves.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/dominio"
	"github.com/mariavlindonc/recienhechoapp/backend/notificaciones/internal/puertos"
)

const (
	// vigenciaClaves es cuánto se recuerda una Idempotency-Key, según el contrato.
	vigenciaClaves = 24 * time.Hour

	// esperaPorOperacion es lo máximo que tarda cada operación, igual que con MongoDB (ADR-011).
	esperaPorOperacion = 2 * time.Second

	prefijoNotificacion = "notificacion:"
	// clavePendientes ordena por próximo intento (en milisegundos) los ids de las pendientes.
	clavePendientes = "notificaciones:pendientes"
)

// crearSiNoExiste guarda la notificación solo si no existía y, si está pendiente,
// la agrega a la cola, en una sola operación atómica.
var crearSiNoExiste = goredis.NewScript(`
if redis.call('SET', KEYS[1], ARGV[1], 'NX') and ARGV[2] ~= '' then
	redis.call('ZADD', KEYS[2], ARGV[2], ARGV[3])
end
return 1
`)

// tomarPendiente aparta la pendiente más vieja lista para enviarse moviendo su
// próximo intento a `hasta`: si dos instancias la piden a la vez, solo una la
// obtiene. Arma la clave de la notificación adentro del script, así que necesita
// un Redis de un solo nodo, como el Key Value de Render.
var tomarPendiente = goredis.NewScript(`
local ids = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, 1)
if #ids == 0 then return false end
local texto = redis.call('GET', ARGV[3] .. ids[1])
if not texto then
	redis.call('ZREM', KEYS[1], ids[1])
	return false
end
redis.call('ZADD', KEYS[1], ARGV[2], ids[1])
return texto
`)

// actualizar reemplaza la notificación si existe y la deja en la cola solo si
// sigue pendiente. Devuelve 0 si no existía.
var actualizar = goredis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
redis.call('SET', KEYS[1], ARGV[1])
if ARGV[2] ~= '' then
	redis.call('ZADD', KEYS[2], ARGV[2], ARGV[3])
else
	redis.call('ZREM', KEYS[2], ARGV[3])
end
return 1
`)

// Repositorio cumple puertos.Repositorio con tres tipos de clave: cada
// notificación en JSON, la cola de pendientes y las claves de idempotencia, que
// vencen solas a las 24 h.
type Repositorio struct {
	cliente *goredis.Client
}

var _ puertos.Repositorio = (*Repositorio)(nil)

// NuevoRepositorio se conecta a la URL dada (redis://host:6379/0).
func NuevoRepositorio(direccion string) (*Repositorio, error) {
	opciones, err := goredis.ParseURL(direccion)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL inválida: %w", err)
	}
	return &Repositorio{cliente: goredis.NewClient(opciones)}, nil
}

// Ping comprueba que Redis responda, para el chequeo de salud.
func (repositorio *Repositorio) Ping(contexto context.Context) error {
	return repositorio.cliente.Ping(contexto).Err()
}

// Cerrar libera las conexiones.
func (repositorio *Repositorio) Cerrar() error { return repositorio.cliente.Close() }

func conLimite(contexto context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(contexto, esperaPorOperacion)
}

// claveIdempotencia no se confunde entre grupos: el nombre del grupo no puede
// tener ":", porque es el separador de NOTIFICACIONES_CLAVES_API.
func claveIdempotencia(grupo, clave string) string {
	return "clave:" + grupo + ":" + clave
}

type documentoClave struct {
	Grupo          string    `json:"grupo"`
	Clave          string    `json:"clave"`
	Huella         string    `json:"huella"`
	IDNotificacion string    `json:"idNotificacion"`
	CreadaEn       time.Time `json:"creadaEn"`
}

type documentoNotificacion struct {
	ID             string     `json:"id"`
	Grupo          string     `json:"grupo"`
	Canal          string     `json:"canal"`
	Destinatario   string     `json:"destinatario"`
	Asunto         string     `json:"asunto"`
	Cuerpo         string     `json:"cuerpo"`
	Estado         string     `json:"estado"`
	Motivo         string     `json:"motivo,omitempty"`
	Intentos       int        `json:"intentos"`
	ProximoIntento time.Time  `json:"proximoIntento"`
	CreadaEn       time.Time  `json:"creadaEn"`
	EnviadaEn      *time.Time `json:"enviadaEn,omitempty"`
}

// ReservarClave usa SET NX: si dos pedidos llegan a la vez con la misma clave,
// solo uno la guarda.
func (repositorio *Repositorio) ReservarClave(contexto context.Context, clave puertos.ClaveIdempotencia) (puertos.ClaveIdempotencia, bool, error) {
	contexto, cancelar := conLimite(contexto)
	defer cancelar()
	nombre := claveIdempotencia(clave.Grupo, clave.Clave)
	texto, err := json.Marshal(documentoClave(clave))
	if err != nil {
		return puertos.ClaveIdempotencia{}, false, fmt.Errorf("codificar clave de idempotencia: %w", err)
	}
	guardo, err := repositorio.cliente.SetNX(contexto, nombre, texto, vigenciaClaves).Result()
	if err != nil {
		return puertos.ClaveIdempotencia{}, false, fmt.Errorf("guardar clave de idempotencia: %w", err)
	}
	if guardo {
		return clave, false, nil
	}

	var guardada documentoClave
	if err := repositorio.leer(contexto, nombre, &guardada); err != nil {
		return puertos.ClaveIdempotencia{}, false, fmt.Errorf("leer clave de idempotencia existente: %w", err)
	}
	return puertos.ClaveIdempotencia(guardada), true, nil
}

// CrearSiNoExiste guarda la notificación; si ya hay una con ese id, no hace nada.
func (repositorio *Repositorio) CrearSiNoExiste(contexto context.Context, notificacion dominio.Notificacion) error {
	contexto, cancelar := conLimite(contexto)
	defer cancelar()
	argumentos, err := argumentosDeGuardado(notificacion)
	if err != nil {
		return err
	}
	if err := crearSiNoExiste.Run(contexto, repositorio.cliente, []string{prefijoNotificacion + notificacion.ID, clavePendientes}, argumentos...).Err(); err != nil {
		return fmt.Errorf("guardar notificación: %w", err)
	}
	return nil
}

// Buscar devuelve la notificación solo si es del grupo que la pide.
func (repositorio *Repositorio) Buscar(contexto context.Context, grupo, id string) (dominio.Notificacion, error) {
	contexto, cancelar := conLimite(contexto)
	defer cancelar()
	var documento documentoNotificacion
	err := repositorio.leer(contexto, prefijoNotificacion+id, &documento)
	if errors.Is(err, goredis.Nil) {
		return dominio.Notificacion{}, dominio.ErrNoEncontrada
	}
	if err != nil {
		return dominio.Notificacion{}, fmt.Errorf("buscar notificación: %w", err)
	}
	if documento.Grupo != grupo {
		return dominio.Notificacion{}, dominio.ErrNoEncontrada
	}
	return aDominio(documento), nil
}

// TomarPendiente aparta una pendiente lista para enviarse hasta `hasta`.
func (repositorio *Repositorio) TomarPendiente(contexto context.Context, ahora, hasta time.Time) (dominio.Notificacion, bool, error) {
	contexto, cancelar := conLimite(contexto)
	defer cancelar()
	texto, err := tomarPendiente.Run(contexto, repositorio.cliente, []string{clavePendientes}, ahora.UnixMilli(), hasta.UnixMilli(), prefijoNotificacion).Text()
	if errors.Is(err, goredis.Nil) {
		return dominio.Notificacion{}, false, nil
	}
	if err != nil {
		return dominio.Notificacion{}, false, fmt.Errorf("tomar notificación pendiente: %w", err)
	}
	var documento documentoNotificacion
	if err := json.Unmarshal([]byte(texto), &documento); err != nil {
		return dominio.Notificacion{}, false, fmt.Errorf("leer notificación pendiente: %w", err)
	}
	notificacion := aDominio(documento)
	// El apartado vive en la cola y no en el JSON; se devuelve igual que MongoDB.
	notificacion.ProximoIntento = hasta.UTC()
	return notificacion, true, nil
}

// Actualizar guarda el estado nuevo y saca de la cola a la que ya no está pendiente.
func (repositorio *Repositorio) Actualizar(contexto context.Context, notificacion dominio.Notificacion) error {
	contexto, cancelar := conLimite(contexto)
	defer cancelar()
	argumentos, err := argumentosDeGuardado(notificacion)
	if err != nil {
		return err
	}
	existia, err := actualizar.Run(contexto, repositorio.cliente, []string{prefijoNotificacion + notificacion.ID, clavePendientes}, argumentos...).Int()
	if err != nil {
		return fmt.Errorf("actualizar notificación: %w", err)
	}
	if existia == 0 {
		return dominio.ErrNoEncontrada
	}
	return nil
}

func (repositorio *Repositorio) leer(contexto context.Context, nombre string, destino any) error {
	texto, err := repositorio.cliente.Get(contexto, nombre).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(texto, destino)
}

// argumentosDeGuardado son los que esperan los scripts: el JSON, el puntaje en la
// cola (vacío si ya no está pendiente) y el id.
func argumentosDeGuardado(notificacion dominio.Notificacion) ([]any, error) {
	texto, err := json.Marshal(aDocumento(notificacion))
	if err != nil {
		return nil, fmt.Errorf("codificar notificación: %w", err)
	}
	puntaje := ""
	if notificacion.Estado == dominio.EstadoPendiente {
		puntaje = fmt.Sprint(notificacion.ProximoIntento.UnixMilli())
	}
	return []any{texto, puntaje, notificacion.ID}, nil
}

func aDocumento(notificacion dominio.Notificacion) documentoNotificacion {
	documento := documentoNotificacion{
		ID:             notificacion.ID,
		Grupo:          notificacion.Grupo,
		Canal:          string(notificacion.Canal),
		Destinatario:   notificacion.Destinatario,
		Asunto:         notificacion.Asunto,
		Cuerpo:         notificacion.Cuerpo,
		Estado:         string(notificacion.Estado),
		Motivo:         notificacion.Motivo,
		Intentos:       notificacion.Intentos,
		ProximoIntento: notificacion.ProximoIntento,
		CreadaEn:       notificacion.CreadaEn,
	}
	if !notificacion.EnviadaEn.IsZero() {
		enviadaEn := notificacion.EnviadaEn
		documento.EnviadaEn = &enviadaEn
	}
	return documento
}

func aDominio(documento documentoNotificacion) dominio.Notificacion {
	notificacion := dominio.Notificacion{
		ID:             documento.ID,
		Grupo:          documento.Grupo,
		Canal:          dominio.Canal(documento.Canal),
		Destinatario:   documento.Destinatario,
		Asunto:         documento.Asunto,
		Cuerpo:         documento.Cuerpo,
		Estado:         dominio.Estado(documento.Estado),
		Motivo:         documento.Motivo,
		Intentos:       documento.Intentos,
		ProximoIntento: documento.ProximoIntento.UTC(),
		CreadaEn:       documento.CreadaEn.UTC(),
	}
	if documento.EnviadaEn != nil {
		notificacion.EnviadaEn = documento.EnviadaEn.UTC()
	}
	return notificacion
}
