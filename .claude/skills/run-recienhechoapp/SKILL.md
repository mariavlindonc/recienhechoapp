---
name: run-recienhechoapp
description: Levantar, correr, probar y manejar Recién hecho (servicio de notificaciones con MongoDB y Mailpit). Usar cuando pidan arrancar o correr la app, probar el contrato de notificaciones con curl, ver el mail en Mailpit, simular la caída del servidor de correo, correr los tests o el test de integración contra MongoDB.
---

El único servicio con lógica es `notificaciones` (los demás y el api-gateway solo exponen `/health`). Se maneja por HTTP con `.claude/skills/run-recienhechoapp/smoke.sh`: levanta MongoDB y Mailpit en Docker, compila y arranca el servicio en la máquina y recorre el contrato con `curl`.

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

## Trampas

- **`docker compose up` no sirve todavía para notificaciones**: el compose solo le pasa `PORT`, y el servicio exige `MONGO_URI`, `NOTIFICACIONES_CLAVES_API`, `SMTP_HOST` y `SMTP_REMITENTE` (issue #17). Por eso el script corre el binario directo.
- **`SMTP_PUERTO` por defecto es 587.** Contra Mailpit hay que poner `1025` o el envío falla.
- **El envío sondea cada 5 s.** El script pone `NOTIFICACIONES_INTERVALO_DE_ENVIO=1s` para que `enviada` llegue en uno o dos segundos.
- **Mongo va en 27018**, como en el comentario de `repositorio_test.go`, para no chocar con un Mongo local en 27017.
- **Mailpit guarda los mails en memoria.** `caida` lo recrea, así que después de correrla la bandeja queda vacía; volver a correr `flujo` para ver un mail.
- **Con Mailpit caído la notificación tarda unos 3,5 minutos en pasar a `fallida`** (reintentos a los 30, 60 y 120 s). `caida` solo muestra el `pendiente`.
- **Los logs todavía son texto**, mezcla de `[GIN-debug]` y `slog` sin JSON ni `trace_id` (eso llega con el paso 3).
- **No hay `jq` en Git Bash**: el script saca `id` y `estado` del JSON con `sed`.
- **`kill` desde Git Bash sí cierra el `.exe` de Windows.** Después de `detener`, `/health` deja de responder y `tasklist` no lo muestra.
