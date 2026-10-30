---
name: run-recienhechoapp
description: Levantar, correr, probar y manejar Recién hecho completo (frontend React, api-gateway y los cinco servicios en el compose). Usar cuando pidan arrancar o correr la app, abrir o sacar capturas del frontend, probar el flujo del comprador en el navegador, cargar budines de ejemplo, pegarle a la API por el gateway con curl, probar notificaciones sola con Mailpit, simular caídas o correr los tests unitarios y de integración.
---

La app entera corre en el compose: frontend en `localhost:5173` (sirve la app y reenvía `/api` al gateway), api-gateway en `localhost:8080` y atrás `usuarios`, `productos`, `pedidos` y `notificaciones` con sus bases. Se maneja con dos herramientas de esta carpeta: `app.sh` levanta y carga datos, y `navegador.ts` maneja el frontend con Chrome sin ventana (protocolo de DevTools, sin dependencias: Bun trae `WebSocket`).

Todas las rutas son relativas a la raíz del repo. Los comandos son para Git Bash en Windows (la máquina de desarrollo) y sirven igual en Linux.

## Prerequisitos

- Go 1.27, Bun 1.4 y Chrome o Edge instalados (`navegador.ts` los busca solos; si no, `NAVEGADOR=/ruta/al/chrome`).
- Docker. En Windows no hay `docker.exe`: está dentro de WSL (Ubuntu). Los scripts usan `docker` si existe y si no `wsl -e docker`; los puertos de WSL se ven en `localhost` de Windows.

## Correr la app completa (camino para agentes)

```bash
bash .claude/skills/run-recienhechoapp/app.sh levantar
bash .claude/skills/run-recienhechoapp/app.sh cargar
MSYS_NO_PATHCONV=1 bun .claude/skills/run-recienhechoapp/navegador.ts flujo
MSYS_NO_PATHCONV=1 bun .claude/skills/run-recienhechoapp/navegador.ts captura / oscuro
MSYS_NO_PATHCONV=1 bun .claude/skills/run-recienhechoapp/navegador.ts eval / "[...document.querySelectorAll('.rejilla md-card h3')].map(titulo => titulo.textContent)"
bash .claude/skills/run-recienhechoapp/app.sh detener
```

| Comando | Qué hace |
|---|---|
| `app.sh levantar` | Arma `.tmp/env` (tu `.env` más lo que falte de `.env.example`, sin tocar tu `.env`), deja WSL despierto, `docker compose up --build -d` y espera al gateway, al frontend y a que `/api` del frontend llegue al gateway. Unos 15 s con las imágenes hechas |
| `app.sh cargar` | Entra como el vendedor del `.env`, crea las categorías Zanahoria, Cítricos y Sin harina de trigo, tres budines (si no estaban) y cupo de 10 para hoy y los próximos 7 días |
| `app.sh estado` | `docker compose ps -a` del proyecto |
| `app.sh detener` | `docker compose down` (los volúmenes y los budines quedan) y suelta WSL |
| `navegador.ts flujo` | Registra un comprador nuevo, abre un budín, lo encarga para mañana y comprueba que quede en "Mis pedidos" resaltado. Saca `flujo-1-catalogo`, `flujo-2-detalle` y `flujo-3-mis-pedidos`. Corta con error si no termina en `/mis-pedidos` |
| `navegador.ts captura <ruta> [oscuro]` | Abre la ruta y saca una captura de 1280x860, en modo claro u oscuro |
| `navegador.ts eval <ruta> '<js>'` | Abre la ruta, evalúa la expresión (acepta `await`) e imprime el resultado en JSON |

Las capturas quedan en `.claude/skills/run-recienhechoapp/.tmp/capturas/`. Mirarlas: que el comando termine bien no garantiza que la pantalla tenga datos.

Cada llamada a `navegador.ts` abre un Chrome nuevo, así que la sesión no pasa de una a otra: una pantalla que pide sesión, sin ella, manda a `/ingresar`. Para entrar, completar el formulario dentro del mismo `eval`:

```bash
MSYS_NO_PATHCONV=1 bun .claude/skills/run-recienhechoapp/navegador.ts eval /ingresar "await (async () => { const llenar = (etiqueta, valor) => { const campo = [...document.querySelectorAll('md-text-field')].find(elemento => elemento.label?.startsWith(etiqueta)); campo.value = valor; campo.dispatchEvent(new InputEvent('input', { bubbles: true, composed: true })) }; llenar('Email', 'vendedor@recienhecho.test'); llenar('Contraseña', 'vendedor-desarrollo'); await new Promise(listo => setTimeout(listo, 200)); document.querySelector('form').requestSubmit(); await new Promise(listo => setTimeout(listo, 2000)); return location.pathname + ' | ' + document.querySelector('main').innerText.slice(0, 120) })()"
```

## Pegarle a la API por el gateway

Con la app levantada. El gateway borra los `X-*-Usuario` que mande el cliente y los arma con el token (ADR-016):

```bash
TOKEN=$(curl -s -X POST localhost:8080/usuarios/sesion -H 'Content-Type: application/json' \
  -d '{"email":"vendedor@recienhecho.test","clave":"vendedor-desarrollo"}' | sed -E 's/.*"token":"([^"]+)".*/\1/')
curl -s localhost:8080/usuarios/yo -H "Authorization: Bearer $TOKEN"
curl -s "localhost:8080/productos/busqueda?q=limon&disponible=true"
curl -s "localhost:8080/pedidos/especiales" -H "Authorization: Bearer $TOKEN"
```

## Notificaciones sola (sin el compose)

`smoke.sh` levanta MongoDB y Mailpit en Docker, compila notificaciones, lo corre en la máquina con dos grupos de claves y recorre el contrato con `curl`. Usa los puertos de Mailpit del compose: bajar el compose antes.

```bash
bash .claude/skills/run-recienhechoapp/smoke.sh arrancar
bash .claude/skills/run-recienhechoapp/smoke.sh flujo
bash .claude/skills/run-recienhechoapp/smoke.sh caida
bash .claude/skills/run-recienhechoapp/smoke.sh detener
```

`flujo`: POST 202, repetido devuelve el mismo id, misma clave con otro cuerpo 409, sin clave 401, GET con la clave de otro grupo 404, espera `enviada` y busca el mail en Mailpit. `caida`: apaga Mailpit, el POST sigue en 202 y queda `pendiente`, y lo vuelve a levantar. El log queda en `.claude/skills/run-recienhechoapp/.tmp/notificaciones.log`. El límite de 60 por minuto no se prueba acá: lo aplica el gateway.

## Tests

```bash
for servicio in backend/*/; do (cd "$servicio" && go test -race ./... && go vet ./...) || break; done; gofmt -l backend
(cd frontend && bunx tsc && bun test)
```

Integración de `productos` contra bases reales (sin las variables se saltean en silencio). Bajar el compose antes: `mysql-productos` también usa el 3307.

```bash
MSYS_NO_PATHCONV=1 wsl -e docker run -d --name mysql-prueba-productos -p 127.0.0.1:3307:3306 -e MYSQL_ROOT_PASSWORD=prueba -e MYSQL_DATABASE=productos_test mysql:8.4.10
MSYS_NO_PATHCONV=1 wsl -e docker run -d --name redis-prueba-productos -p 127.0.0.1:6380:6379 redis:7.4.11
MSYS_NO_PATHCONV=1 wsl -e docker run -d --name meili-prueba-productos -p 127.0.0.1:7701:7700 -e MEILI_MASTER_KEY=clave-de-prueba-de-16 getmeili/meilisearch:v1.54.3
until MSYS_NO_PATHCONV=1 wsl -e docker exec mysql-prueba-productos mysqladmin ping -h127.0.0.1 --protocol=tcp -uroot -pprueba >/dev/null 2>&1; do sleep 2; done
(cd backend/productos && MYSQL_DSN_TEST="root:prueba@tcp(127.0.0.1:3307)/productos_test" REDIS_URL_TEST=redis://127.0.0.1:6380/0 MEILI_URL_TEST=http://127.0.0.1:7701 MEILI_CLAVE_TEST=clave-de-prueba-de-16 go test -race -count=1 ./...)
MSYS_NO_PATHCONV=1 wsl -e docker rm -f mysql-prueba-productos redis-prueba-productos meili-prueba-productos
```

Con `smoke.sh arrancar` corriendo, la integración de notificaciones: `(cd backend/notificaciones && MONGO_URI_TEST=mongodb://localhost:27018 go test -count=1 ./internal/adaptadores/mongodb/...)`.

## Tests de carga (k6)

Escenarios en `tests/carga/` ([README](../../../tests/carga/README.md)). Con la app levantada y `app.sh cargar` hecho, k6 corre en el compose con el perfil `carga`. Antes hay que subir `NOTIFICACIONES_LIMITE_POR_MINUTO` en el env que usa el compose y recrear el gateway; si no, notificaciones da `429`. Un escenario completo tarda 10 minutos; para probar que el script anda, `--vus 1 --iterations 1`:

```bash
MSYS_NO_PATHCONV=1 wsl -e bash -c "cd '<raíz en /mnt/c/...>' && docker compose --env-file .claude/skills/run-recienhechoapp/.tmp/env -p recienhechoapp --profile carga run --rm k6 run --vus 1 --iterations 1 /carga/catalogo.js"
```

Los resultados también van a Prometheus (`k6_http_req_duration_p95`, `k6_http_reqs_total`), etiquetados con `testid` si se pasa `--tag testid=<nombre>`.

## Correr a mano (personas)

`cp .env.example .env` una vez y `docker compose up --build -d`; después http://localhost:5173. Para el frontend con recarga automática, `cd frontend && bun run dev` (Vite en el 5173 reenvía `/api` a `localhost:8080`): no dejarlo corriendo junto con el compose (ver trampas).

## Trampas

- **Git Bash cambia las rutas que empiezan con `/`** cuando llama a un `.exe` como Bun: `navegador.ts captura /` llegaba como `C:/Program Files/Git/` y Chrome sacaba captura de esa carpeta. Siempre `MSYS_NO_PATHCONV=1` delante; el script corta con un aviso si le llega una ruta así.
- **`git rev-parse --show-toplevel` devuelve `C:/Users/...` en Git Bash**, no `/c/Users/...`. `app.sh` convierte las dos formas a `/mnt/c/...` para WSL.
- **Un `bun run dev` olvidado le gana al contenedor.** Vite escucha en `[::1]:5173` y el compose en `127.0.0.1:5173`; `localhost` va primero a `::1`, así que el navegador ve Vite y `/api` devuelve el HTML de la app (`Unexpected token '<'`). Al cortar `bun run dev` desde una herramienta puede quedar vivo el `node.exe` hijo: buscarlo con `netstat -ano | grep :5173`. `app.sh levantar` lo detecta y corta.
- **Chrome sin ventana copia el tema de Windows.** Con Windows en oscuro, todas las capturas salían oscuras. `navegador.ts` fija claro u oscuro en cada llamada.
- **En Windows el Docker de WSL se apaga por inactividad** cuando no queda ningún proceso de `wsl` vivo, y se lleva los contenedores. `app.sh` deja un `wsl -e sleep` en segundo plano (PID en `.tmp/wsl.pid`) y lo mata en `detener`.
- **Un contenedor viejo con el mismo nombre frena el compose** (`Conflict. The container name "/recienhecho-frontend" is already in use`). Pasa con contenedores de otro proyecto de compose (otro `-p` o carpeta). Ver de dónde es con `wsl -e docker ps -a --filter name=recienhecho`; si está parado y es de otra corrida, borrarlo.
- **`docker compose ps --format '...'` pierde las comillas** al pasar por `wsl -e bash -c`: `app.sh estado` usa `ps -a` sin formato.
- **La búsqueda tarda unos 3 s en ver un cambio** (bandeja de Meilisearch, ADR-008). Recién cargados o borrados, los budines pueden no aparecer en el catálogo.
- **Los budines quedan en los volúmenes** después de `detener`. `cargar` no los duplica. Los precios son de relleno a propósito: no sabemos los reales.
- **Los `md-text-field` no son `input`:** para llenarlos desde JS hay que poner `.value` y disparar un `InputEvent` con `composed: true`. Buscarlos por `.label` (es propiedad, no atributo).
- **Enter no siempre envía los formularios de material-esm** (pasó en el navegador del panel de Claude): `navegador.ts` usa `form.requestSubmit()`.
- **MySQL arranca dos veces.** La imagen levanta un servidor temporal y después se reinicia. Esperar con `mysqladmin ping --protocol=tcp`, porque el temporal no escucha en la red.
- **Mailpit guarda los mails en memoria**: `smoke.sh caida` lo recrea y la bandeja queda vacía. Con Mailpit caído, una notificación tarda unos 3,5 minutos en pasar a `fallida`.
- **`SMTP_PUERTO` por defecto es 587**; contra Mailpit va `1025`. **La clave maestra de Meilisearch** tiene que tener al menos 16 caracteres.
- **Los `.sh` se bajan en LF** por el `.gitattributes` de esta carpeta: con CRLF, bash de WSL falla.

## Si algo falla

| Síntoma | Qué hacer |
|---|---|
| `FALLA: el compose no levantó` | Mirar `.claude/skills/run-recienhechoapp/.tmp/compose.log` (buscar `Error`). El caso visto fue el conflicto de nombre de arriba |
| `FALLA: .../api no llega al gateway` | Otro proceso en el 5173 (trampa de Vite). `netstat -ano \| grep :5173` y cerrarlo |
| El catálogo muestra `Unexpected token '<'` | Lo mismo: el navegador está viendo Vite, no el contenedor |
| `la ruta llegó como C:/...` | Faltó `MSYS_NO_PATHCONV=1` |
| `flujo` corta en el registro (`sesion=false`) | Mirar `flujo-1-catalogo.png`: muestra el error del formulario |
