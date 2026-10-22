#!/usr/bin/env bash
# Levanta notificaciones con MongoDB y Mailpit reales y lo maneja por HTTP.
# Uso (desde la raíz del repo, en Git Bash o Linux):
#   smoke.sh arrancar   MongoDB y Mailpit en Docker, compila y arranca notificaciones
#   smoke.sh flujo      recorre el contrato con curl y verifica el mail en Mailpit
#   smoke.sh caida      apaga Mailpit y muestra que el POST sigue respondiendo 202
#   smoke.sh detener    apaga todo
set -u

raiz=$(git rev-parse --show-toplevel)
temporal="$raiz/.claude/skills/run-recienhechoapp/.tmp"
mkdir -p "$temporal"
puerto=${NOTIFICACIONES_PORT:-8084}
base="http://localhost:$puerto"
clave_a="clave-grupo-a"
clave_b="clave-grupo-b"

# En Windows el Docker vive dentro de WSL; sus puertos se ven igual en localhost.
if command -v docker >/dev/null 2>&1; then docker_cmd=(docker); else docker_cmd=(wsl -e docker); fi

fallar() { echo "FALLA: $*" >&2; exit 1; }

esperar_url() {
	for _ in $(seq 1 "${2:-30}"); do
		curl -sf -o /dev/null "$1" && return 0
		sleep 1
	done
	return 1
}

arrancar() {
	"${docker_cmd[@]}" rm -f rh-mongo rh-mailpit >/dev/null 2>&1
	"${docker_cmd[@]}" run -d --rm --name rh-mongo -p 27018:27017 mongo:8 >/dev/null || fallar "no arrancó MongoDB"
	"${docker_cmd[@]}" run -d --rm --name rh-mailpit -p 1025:1025 -p 8025:8025 axllent/mailpit >/dev/null || fallar "no arrancó Mailpit"
	esperar_url http://localhost:8025/api/v1/info || fallar "Mailpit no responde en 8025"

	(cd "$raiz/backend/notificaciones" && go build -o "$temporal/notificaciones" .) || fallar "no compila"
	PORT=$puerto \
		MONGO_URI=mongodb://localhost:27018 \
		NOTIFICACIONES_CLAVES_API="grupo-a:$clave_a,grupo-b:$clave_b" \
		NOTIFICACIONES_INTERVALO_DE_ENVIO=1s \
		SMTP_HOST=localhost SMTP_PUERTO=1025 \
		SMTP_REMITENTE="Recién hecho <notificaciones@recienhecho.local>" \
		"$temporal/notificaciones" >"$temporal/notificaciones.log" 2>&1 &
	echo $! >"$temporal/notificaciones.pid"
	esperar_url "$base/health" 40 || { tail -20 "$temporal/notificaciones.log"; fallar "notificaciones no responde /health"; }
	echo "OK notificaciones en $base (log: $temporal/notificaciones.log), Mailpit en http://localhost:8025"
}

# pedir METODO RUTA CLAVE [IDEMPOTENCIA] [CUERPO]: deja el código en $codigo y el cuerpo en $respuesta.
pedir() {
	local argumentos=(-s -o "$temporal/respuesta.json" -w '%{http_code}' -X "$1" "$base$2")
	[ -n "$3" ] && argumentos+=(-H "X-API-Key: $3")
	[ -n "${4:-}" ] && argumentos+=(-H "Idempotency-Key: $4")
	[ -n "${5:-}" ] && argumentos+=(-H "Content-Type: application/json" -d "$5")
	codigo=$(curl "${argumentos[@]}")
	respuesta=$(cat "$temporal/respuesta.json")
}

campo() { sed -n "s/.*\"$1\":\"\([^\"]*\)\".*/\1/p" <<<"$respuesta"; }

comprobar() { [ "$codigo" = "$1" ] || fallar "$2: esperaba $1 y vino $codigo: $respuesta"; echo "OK $2 -> $codigo"; }

flujo() {
	local idempotencia asunto cuerpo
	idempotencia="smoke-$(date +%s%N)"
	asunto="Smoke $idempotencia"
	cuerpo="{\"canal\":\"email\",\"destinatario\":\"cliente@ejemplo.com\",\"asunto\":\"$asunto\",\"cuerpo\":\"Ya podés retirarlo.\"}"

	pedir POST /notificaciones "$clave_a" "$idempotencia" "$cuerpo"
	comprobar 202 "POST nuevo"
	local identificador
	identificador=$(campo id)
	[ -n "$identificador" ] || fallar "el POST no devolvió id: $respuesta"

	pedir POST /notificaciones "$clave_a" "$idempotencia" "$cuerpo"
	comprobar 202 "POST repetido"
	[ "$(campo id)" = "$identificador" ] || fallar "el POST repetido devolvió otro id"
	echo "OK mismo id $identificador"

	pedir POST /notificaciones "$clave_a" "$idempotencia" "${cuerpo/Ya podés/Otro texto}"
	comprobar 409 "misma clave, otro cuerpo"

	pedir POST /notificaciones "" "$idempotencia" "$cuerpo"
	comprobar 401 "sin X-API-Key"

	pedir GET "/notificaciones/$identificador" "$clave_b"
	comprobar 404 "GET con la clave de otro grupo"

	for _ in $(seq 1 20); do
		pedir GET "/notificaciones/$identificador" "$clave_a"
		[ "$(campo estado)" = "enviada" ] && break
		sleep 1
	done
	[ "$(campo estado)" = "enviada" ] || fallar "no pasó a enviada: $respuesta"
	echo "OK estado enviada"

	curl -s "http://localhost:8025/api/v1/search?query=subject:%22$idempotencia%22" | grep -q "\"Subject\":\"$asunto\"" \
		|| fallar "el mail no aparece en Mailpit"
	echo "OK mail \"$asunto\" en Mailpit (http://localhost:8025)"
}

caida() {
	"${docker_cmd[@]}" stop rh-mailpit >/dev/null
	local idempotencia
	idempotencia="caida-$(date +%s%N)"
	pedir POST /notificaciones "$clave_a" "$idempotencia" \
		'{"canal":"email","destinatario":"cliente@ejemplo.com","asunto":"Con Mailpit caido","cuerpo":"Prueba."}'
	comprobar 202 "POST con Mailpit caído"
	local identificador
	identificador=$(campo id)
	sleep 3
	pedir GET "/notificaciones/$identificador" "$clave_a"
	echo "OK estado con Mailpit caído: $(campo estado) (reintenta a los 30, 60 y 120 s y después queda fallida)"
	"${docker_cmd[@]}" run -d --rm --name rh-mailpit -p 1025:1025 -p 8025:8025 axllent/mailpit >/dev/null
	echo "OK Mailpit levantado de nuevo"
}

detener() {
	[ -f "$temporal/notificaciones.pid" ] && kill "$(cat "$temporal/notificaciones.pid")" 2>/dev/null
	rm -f "$temporal/notificaciones.pid"
	"${docker_cmd[@]}" rm -f rh-mongo rh-mailpit >/dev/null 2>&1
	echo "OK todo apagado"
}

case "${1:-}" in
arrancar | flujo | caida | detener) "$1" ;;
*) echo "uso: $0 arrancar|flujo|caida|detener" >&2; exit 2 ;;
esac
