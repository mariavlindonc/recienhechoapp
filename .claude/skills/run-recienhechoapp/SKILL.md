---
name: run-recienhechoapp
description: Levantar, correr, probar y manejar Recién hecho (notificaciones con MongoDB y Mailpit; productos con MySQL, Redis y Meilisearch; el compose completo). Usar cuando pidan arrancar o correr la app, probar el contrato de notificaciones o el catálogo con curl, ver el mail en Mailpit, simular caídas, correr los tests o los tests de integración contra MongoDB, MySQL, Redis o Meilisearch.
---

`notificaciones`, `pedidos` y `productos` tienen lógica; `usuarios` solo expone `/health`. `notificaciones` se maneja solo con `.claude/skills/run-recienhechoapp/smoke.sh`: levanta MongoDB y Mailpit en Docker, compila y arranca el servicio en la máquina y recorre el contrato con `curl`. Para `productos` y el flujo completo, ver la sección "Productos y el compose completo".

Todas las rutas son relativas a la raíz del repo. Los comandos son para Git Bash (Windows) o Linux.

## Prerequisitos

- Go 1.27 (`go version`).
- Docker. En la máquina de desarrollo Windows no hay `docker.exe`: está dentro de WSL (Ubuntu). El script usa `docker` si existe y si no `wsl -e docker`. Los puertos que publica WSL se ven en `localhost` de Windows.
- Imágenes, una sola vez:

```bash
wsl -e sh -c 'docker pull -q mongo:8 && docker pull -q axllent/mailpit'
```

## Correr (camino para agentes)

```bash
bash .claude/skills/run-recienhechoapp/smoke.sh arrancar
bash .claude/skills/run-recienhechoapp/smoke.sh flujo
bash .claude/skills/run-recienhechoapp/smoke.sh caida
bash .claude/skills/run-recienhechoapp/smoke.sh detener
```

| Comando | Qué hace |
|---|---|
| `arrancar` | MongoDB en `localhost:27018`, Mailpit en `1025` (SMTP) y `8025` (web), compila `backend/notificaciones` y lo arranca en `localhost:8084` con dos grupos (`grupo-a:clave-grupo-a`, `grupo-b:clave-grupo-b`). Espera a `/health` |
| `flujo` | `POST` 202, repetido devuelve el mismo id, otro cuerpo con la misma clave 409, sin clave 401, `GET` con la clave del otro grupo 404, espera `enviada` y busca el mail en la API de Mailpit. Corta en la primera falla con `FALLA: ...` |
| `caida` | Apaga Mailpit, hace un `POST` (sigue en 202 y queda `pendiente`) y vuelve a levantar Mailpit |
| `detener` | Mata el servicio y borra los dos contenedores |

El log del servicio queda en `.claude/skills/run-recienhechoapp/.tmp/notificaciones.log`. Los mails se ven en http://localhost:8025.

Para pegarle a mano con el servicio arriba:

```bash
curl -i -X POST http://localhost:8084/notificaciones -H "X-API-Key: clave-grupo-a" -H "Idempotency-Key: prueba-1" -H "Content-Type: application/json" -d '{"canal":"email","destinatario":"cliente@ejemplo.com","asunto":"Hola","cuerpo":"Prueba."}'
```

## Tests

```bash
for servicio in backend/*/; do (cd "$servicio" && go test ./... && go vet ./...) || break; done; gofmt -l backend
```

Integración contra el MongoDB que levanta `arrancar` (sin `MONGO_URI_TEST` los 7 tests se saltean en silencio):

```bash
(cd backend/notificaciones && MONGO_URI_TEST=mongodb://localhost:27018 go test -count=1 ./internal/adaptadores/mongodb/...)
```

## Productos y el compose completo

Primero dejar WSL despierto en segundo plano, si no Docker se apaga entre comando y comando y se lleva los contenedores:

```bash
wsl -e sleep 3600   # en segundo plano (run_in_background)
```

Tests de integración de `productos`, cada uno contra su base (se saltean sin la variable):

```bash
MSYS_NO_PATHCONV=1 wsl -e docker run -d --name mysql-prueba-productos -p 127.0.0.1:3307:3306 -e MYSQL_ROOT_PASSWORD=prueba -e MYSQL_DATABASE=productos_test mysql:8.4.10
MSYS_NO_PATHCONV=1 wsl -e docker run -d --name redis-prueba-productos -p 127.0.0.1:6380:6379 redis:7.4.11
MSYS_NO_PATHCONV=1 wsl -e docker run -d --name meili-prueba-productos -p 127.0.0.1:7701:7700 -e MEILI_MASTER_KEY=clave-de-prueba-de-16 getmeili/meilisearch:v1.54.3
# esperar a MySQL por TCP (ver trampas) y después:
(cd backend/productos && MYSQL_DSN_TEST="root:prueba@tcp(127.0.0.1:3307)/productos_test" REDIS_URL_TEST=redis://127.0.0.1:6380/0 MEILI_URL_TEST=http://127.0.0.1:7701 MEILI_CLAVE_TEST=clave-de-prueba-de-16 go test -race -count=1 ./...)
```

Compose completo: escribir un script y correrlo entero con `MSYS_NO_PATHCONV=1 wsl -e bash script.sh`. Si el `.env` del usuario no tiene todas las variables, no tocarlo: armar uno temporal con su `.env` más lo que falte de `.env.example` y pasarlo con `docker compose --env-file`. Pasos útiles, por el gateway (`localhost:8080`):

```bash
V=(-H 'X-Id-Usuario: v1' -H 'X-Rol-Usuario: vendedor' -H 'Content-Type: application/json')
curl -s "${V[@]}" -X POST localhost:8080/categorias -d '{"nombre":"Tortas"}'
curl -s "${V[@]}" -X POST localhost:8080/productos -d '{"nombre":"Torta de limón","categoriaId":"<id>","precioCentavos":850000,"anticipacionMinimaDias":2,"disponible":true}'
curl -s -H 'X-Id-Usuario: v1' -H 'X-Rol-Usuario: vendedor' -F "imagen=@foto.png" localhost:8080/productos/<id>/imagen
curl -s "localhost:8080/productos/busqueda?q=limon&categoria=Tortas&disponible=true&orden=precio_asc"   # a los ~3 s
docker compose exec -T productos wget -qO- localhost:9464/metrics | grep cache_   # aciertos y fallos
docker compose exec -T productos /servicio reindexar
```

Caídas para probar: `docker compose stop meilisearch` (búsqueda en 503, catálogo sigue, los cambios se aplican al volver) y `docker compose stop redis-productos` (el catálogo lee de MySQL).

## Trampas

- **`docker compose up --build -d` también levanta notificaciones** (con `cp .env.example .env` antes), con Mongo, Mailpit y las claves de desarrollo del `.env.example`. `smoke.sh` corre el binario directo para no depender de Docker en el build y para tener dos grupos de claves.
- **En Windows el Docker de WSL se apaga por inactividad** cuando termina el comando de `wsl`, y con él los contenedores del compose (salen con código 0 tras un apagado ordenado). Para una prueba larga con compose, meter todo en un solo `wsl -e bash script.sh`, y desde Git Bash usar `MSYS_NO_PATHCONV=1` para que no cambie las rutas `/mnt/c/...`.
- **`SMTP_PUERTO` por defecto es 587.** Contra Mailpit hay que poner `1025` o el envío falla.
- **El envío sondea cada 5 s.** El script pone `NOTIFICACIONES_INTERVALO_DE_ENVIO=1s` para que `enviada` llegue en uno o dos segundos.
- **Mongo va en 27018**, como en el comentario de `repositorio_test.go`, para no chocar con un Mongo local en 27017.
- **Mailpit guarda los mails en memoria.** `caida` lo recrea, así que después de correrla la bandeja queda vacía; volver a correr `flujo` para ver un mail.
- **Con Mailpit caído la notificación tarda unos 3,5 minutos en pasar a `fallida`** (reintentos a los 30, 60 y 120 s). `caida` solo muestra el `pendiente`.
- **Los logs todavía son texto**, mezcla de `[GIN-debug]` y `slog` sin JSON ni `trace_id` (eso llega con el paso 3).
- **No hay `jq` en Git Bash**: el script saca `id` y `estado` del JSON con `sed`.
- **`smoke.sh` se baja siempre en LF** por el `.gitattributes` de esta carpeta. Con `core.autocrlf=true` llegaba con CRLF: Git Bash lo tolera, pero bash en Linux o WSL no.
- **`kill` desde Git Bash sí cierra el `.exe` de Windows.** Después de `detener`, `/health` deja de responder y `tasklist` no lo muestra.
- **MySQL arranca dos veces.** La imagen levanta un servidor temporal para inicializarse y después se reinicia. Un `select 1` por socket le pega al temporal y da un "listo" falso: esperar con `mysqladmin ping -h127.0.0.1 --protocol=tcp`, porque el temporal no escucha en la red.
- **La clave maestra de Meilisearch tiene que tener al menos 16 caracteres.**
- **Los puertos de prueba de `productos` (3307, 6380, 7701) chocan con el compose** (`mysql-productos` también usa 3307). Borrar los contenedores de prueba antes de levantar el compose.
