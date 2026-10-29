#!/usr/bin/env bash
# Levanta la app completa (compose: backend, frontend y herramientas) y la deja lista para manejar.
# Uso (desde la raíz del repo, en Git Bash o Linux):
#   app.sh levantar   arma el .env temporal, levanta el compose y espera al gateway y al frontend
#   app.sh cargar     crea las categorías, tres budines de ejemplo y cupo para la semana
#   app.sh estado     muestra los contenedores
#   app.sh detener    baja el compose (los volúmenes quedan)
set -u

raiz=$(git rev-parse --show-toplevel)
temporal="$raiz/.claude/skills/run-recienhechoapp/.tmp"
mkdir -p "$temporal"
gateway=http://localhost:8080
frontend=http://localhost:5173

fallar() { echo "FALLA: $*" >&2; exit 1; }

# En Windows Docker vive dentro de WSL: los comandos corren ahí y las rutas pasan a /mnt/c/...
if command -v docker >/dev/null 2>&1; then
	compose() { (cd "$raiz" && docker compose --env-file "$temporal/env" -p recienhechoapp "$@"); }
else
	# git rev-parse devuelve C:/Users/... en Git Bash; WSL lo ve en /mnt/c/Users/...
	en_wsl() { echo "$1" | sed -E 's#^([a-zA-Z]):/#/mnt/\L\1/#; s#^/([a-zA-Z])/#/mnt/\L\1/#'; }
	compose() {
		MSYS_NO_PATHCONV=1 wsl -e bash -c "cd '$(en_wsl "$raiz")' && docker compose --env-file '$(en_wsl "$temporal/env")' -p recienhechoapp $*"
	}
fi

esperar_url() {
	for _ in $(seq 1 "${2:-60}"); do
		curl -sf -o /dev/null "$1" && return 0
		sleep 2
	done
	return 1
}

# El .env del usuario no se toca: se copia y se le agrega lo que falte de .env.example.
armar_env() {
	: >"$temporal/env"
	[ -f "$raiz/.env" ] && cat "$raiz/.env" >>"$temporal/env"
	grep -E '^[A-Z_]+=' "$raiz/.env.example" | while IFS= read -r linea; do
		grep -q "^${linea%%=*}=" "$temporal/env" || echo "$linea" >>"$temporal/env"
	done
}

# Sin un proceso de WSL vivo, Docker se apaga por inactividad y se lleva los contenedores.
mantener_wsl() {
	command -v docker >/dev/null 2>&1 && return
	[ -f "$temporal/wsl.pid" ] && kill -0 "$(cat "$temporal/wsl.pid")" 2>/dev/null && return
	wsl -e sleep 86400 >/dev/null 2>&1 &
	echo $! >"$temporal/wsl.pid"
}

levantar() {
	armar_env
	mantener_wsl
	compose up --build -d >"$temporal/compose.log" 2>&1 || { tail -20 "$temporal/compose.log"; fallar "el compose no levantó"; }
	esperar_url "$gateway/categorias" || fallar "el gateway no responde en $gateway (ver: app.sh estado)"
	esperar_url "$frontend/" || fallar "el frontend no responde en $frontend"
	# Un "bun run dev" olvidado escucha en [::1]:5173 y le gana al contenedor: /api devuelve HTML.
	curl -s "$frontend/api/categorias" | grep -q '^\[' ||
		fallar "$frontend/api no llega al gateway: hay otro proceso en el 5173 (netstat -ano | grep :5173)"
	echo "OK gateway en $gateway, frontend en $frontend, Mailpit en http://localhost:8025"
}

token_del_vendedor() {
	local email clave
	email=$(grep '^VENDEDOR_EMAIL=' "$temporal/env" | cut -d= -f2-)
	clave=$(grep '^VENDEDOR_CLAVE=' "$temporal/env" | cut -d= -f2-)
	curl -s -X POST "$gateway/usuarios/sesion" -H 'Content-Type: application/json' \
		-d "{\"email\":\"$email\",\"clave\":\"$clave\"}" | sed -nE 's/.*"token":"([^"]+)".*/\1/p'
}

cargar() {
	[ -f "$temporal/env" ] || fallar "primero app.sh levantar"
	local token
	token=$(token_del_vendedor)
	[ -n "$token" ] || fallar "no se pudo iniciar sesión como vendedor"
	local autorizado=(-H "Authorization: Bearer $token" -H 'Content-Type: application/json')
	id_de_categoria() { curl -s "$gateway/categorias" | grep -o "{[^}]*\"nombre\":\"$1\"[^}]*}" | sed -E 's/.*"id":"([^"]+)".*/\1/' | head -1; }
	for nombre in Zanahoria Cítricos "Sin harina de trigo"; do
		[ -n "$(id_de_categoria "$nombre")" ] || curl -s -o /dev/null -X POST "$gateway/categorias" "${autorizado[@]}" -d "{\"nombre\":\"$nombre\"}"
	done
	producto() {
		curl -s -o /dev/null -w "  $1: %{http_code}\n" -X POST "$gateway/productos" "${autorizado[@]}" \
			-d "{\"nombre\":\"$1\",\"descripcion\":\"$2\",\"categoriaId\":\"$(id_de_categoria "$3")\",\"precioCentavos\":$4,\"anticipacionMinimaDias\":1,\"aptoCeliacos\":$5,\"disponible\":true}"
	}
	if curl -s "$gateway/productos" | grep -q 'Budín de limón'; then
		echo "los budines ya estaban"
	else
		# Precios de relleno a propósito: no sabemos los reales.
		producto "Budín de limón" "Con semillas de amapola y limones caramelizados." "Cítricos" 12312300 false
		producto "Budín de zanahoria" "Con frutos secos y glasé de naranja." "Zanahoria" 45645600 false
		producto "Budín de zanahoria sin TACC" "Masa de zanahoria sin harina de trigo, con cranberries." "Sin harina de trigo" 78978900 true
	fi
	# Cupo para hoy y los próximos 7 días, así se puede encargar enseguida.
	for dias in $(seq 0 7); do
		curl -s -o /dev/null -X PUT "$gateway/capacidad/$(date -d "+$dias days" +%F)" "${autorizado[@]}" -d '{"capacidad":10}'
	done
	echo "OK budines y cupo cargados (la búsqueda los ve a los ~3 s)"
}

detener() {
	[ -f "$temporal/env" ] && compose down >/dev/null 2>&1
	[ -f "$temporal/wsl.pid" ] && kill "$(cat "$temporal/wsl.pid")" 2>/dev/null
	rm -f "$temporal/wsl.pid"
	echo "OK compose abajo"
}

case "${1:-}" in
levantar) levantar ;;
cargar) cargar ;;
estado) compose ps -a ;;
detener) detener ;;
*) fallar "uso: app.sh levantar|cargar|estado|detener" ;;
esac
