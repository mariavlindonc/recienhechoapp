# AGENTS.md

Instrucciones para agentes de código (Claude Code, Antigravity, Codex, OpenCode) y personas. `CLAUDE.md` y `GEMINI.md` solo remiten a este archivo.

## Proyecto

**Recién hecho**: e-commerce de pastelería a pedido. Microservicios en Go con Gin, orquestados con Docker Compose. Requisitos en [SPEC.md](docs/SPEC.md).

```
backend/    api-gateway, usuarios, productos, pedidos, notificaciones (cada uno con su go.mod y Dockerfile)
frontend/   interfaz web en React 19, TypeScript, material-esm y Bun (ADR-014); habla con los servicios solo por el api-gateway
docs/       arquitectura, ADRs, contratos, postmortems
observabilidad/  configuración de Prometheus (lectura de métricas y alertas) y de Grafana (fuentes y tablero)
```

## Comandos

```bash
cp .env.example .env                 # una sola vez
docker compose up --build -d         # levantar
docker compose down                  # bajar

# Tests (con detector de carreras) y lint, desde la raíz (en Windows, con Git Bash). gofmt no tiene que listar nada.
for servicio in backend/*/; do (cd "$servicio" && go test -race ./... && go vet ./...) || break; done; gofmt -l backend
```

`-race` activa el detector de carreras de Go: avisa si dos goroutines usan el mismo dato al mismo tiempo sin protección. Necesita un compilador de C (`gcc`) y `CGO_ENABLED=1`, que es el valor por defecto. En Windows se instala con [MSYS2](https://www.msys2.org/) (`pacman -S mingw-w64-ucrt-x86_64-gcc`), agregando su carpeta `ucrt64/bin` al `PATH`; en macOS viene con las herramientas de Xcode (`xcode-select --install`).

## Convenciones

- Código con `gofmt` y `go vet` limpio.
- Identificadores, comentarios y mensajes en español, sin variables de una letra.
- Errores manejados y devueltos con contexto (`fmt.Errorf("...: %w", err)`).
- Configuración por variables de entorno; nada sensible en el código. Cada servicio las lee en su `configuracion.go`, con una función `leerConfiguracion` que valida todo al arrancar y avisa todos los problemas juntos.
- Un servicio no importa código de otro. El código de logs, trazas y métricas se copia y respeta las convenciones de [ADR-006](docs/adr/ADR-006-observabilidad.md#convenciones-compartidas-entre-servicios).
- Los servicios se arman igual siempre que su arquitectura lo permita: configuración en `configuracion.go`, apagado ordenado ante SIGTERM (`signal.NotifyContext` y `http.Server.Shutdown`) y el mismo orden de middlewares.
- Dependencias de Go e imágenes de Docker con versión exacta, nunca `latest` ni rangos, elegida entre las publicadas hace al menos dos semanas: las recién salidas tienen más riesgo de errores y de problemas de seguridad sin detectar. Se actualizan periódicamente.
- Commits con [Conventional Commits](https://www.conventionalcommits.org/es/) en español: `feat(productos): agrega listado de catálogo`.
- Ramas con git flow; detalle en [CONTRIBUTING.md](docs/CONTRIBUTING.md).

## Infraestructura

Cada dependencia que se agregue se lista acá con su uso, puerto y URL local.
Las herramientas de desarrollo (MongoDB, MySQL, Redis, Meilisearch, RabbitMQ, Mailpit, Jaeger, Prometheus, Grafana y el mock de Prism) publican su puerto solo en `127.0.0.1`: se usan desde la propia computadora y no quedan expuestas a la red local ([ADR-006](docs/adr/ADR-006-observabilidad.md)). Los servicios no publican puerto: se entra solo por el api-gateway, en `localhost:8080` ([ADR-001](docs/adr/ADR-001-limites-de-los-servicios.md), [ADR-012](docs/adr/ADR-012-balanceo-de-carga.md)).

| Dependencia | Uso | Puerto | URL local |
|---|---|---|---|
| MongoDB (`mongo:8`) | Base de `notificaciones` ([ADR-002](docs/adr/ADR-002-persistencia.md)) | `NOTIFICACIONES_MONGO_PORT` (27017) | `mongodb://localhost:27017` (usuario y clave en `.env`) |
| MongoDB (`mongo:8`, replica set) | Base de `pedidos`, con transacciones ([ADR-015](docs/adr/ADR-015-consistencia-y-concurrencia.md)) | `PEDIDOS_MONGO_PORT` (27020) | `mongodb://localhost:27020/?directConnection=true` (usuario y clave en `.env`) |
| MySQL (`mysql:8.4.10`) | Base de `productos`, con GORM ([ADR-002](docs/adr/ADR-002-persistencia.md)) | `PRODUCTOS_MYSQL_PORT` (3307) | `localhost:3307`, base `productos` (usuario y clave en `.env`) |
| MySQL (`mysql:8.4.10`) | Base de `usuarios`, con GORM ([ADR-002](docs/adr/ADR-002-persistencia.md), [ADR-016](docs/adr/ADR-016-autenticacion.md)) | `USUARIOS_MYSQL_PORT` (3308) | `localhost:3308`, base `usuarios` (usuario y clave en `.env`) |
| Redis (`redis:7.4.11`) | Caché del catálogo de `productos` ([ADR-009](docs/adr/ADR-009-cache.md)) | `PRODUCTOS_REDIS_PORT` (6379) | `redis://localhost:6379` |
| Meilisearch (`getmeili/meilisearch:v1.54.3`) | Búsqueda de productos ([ADR-008](docs/adr/ADR-008-busqueda.md)) | `MEILI_PORT` (7700) | http://localhost:7700 (clave en `.env`, `MEILI_CLAVE`) |
| RabbitMQ (`rabbitmq:4.3.5-management-alpine`) | Eventos de `pedidos` a `notificaciones` ([ADR-003](docs/adr/ADR-003-comunicacion-entre-servicios.md)) | `RABBITMQ_WEB_PORT` (15672, solo la web) | http://localhost:15672 (usuario y clave en `.env`) |
| Mailpit (`axllent/mailpit`) | Buzón falso para desarrollo ([ADR-005](docs/adr/ADR-005-envio-de-email.md)) | `MAILPIT_SMTP_PORT` (1025) y `MAILPIT_WEB_PORT` (8025) | http://localhost:8025 |
| Prometheus (`prom/prometheus:v3.15.0`) | Lee `/metrics` de los servicios (puerto interno 9464) y evalúa las alertas ([ADR-006](docs/adr/ADR-006-observabilidad.md)) | `PROMETHEUS_WEB_PORT` (9090) | http://localhost:9090 |
| Grafana (`grafana/grafana:13.2.3`) | Tablero de métricas y acceso a las trazas ([ADR-006](docs/adr/ADR-006-observabilidad.md)) | `GRAFANA_WEB_PORT` (3000) | http://localhost:3000 (sin usuario, solo lectura; admin con `GRAFANA_ADMIN_CLAVE`) |
| Jaeger (`jaegertracing/jaeger:2.22.0`) | Trazas de api-gateway y notificaciones por OTLP ([ADR-006](docs/adr/ADR-006-observabilidad.md)) | `JAEGER_WEB_PORT` (16686) | http://localhost:16686 |
| k6 (`grafana/k6:2.2.0`) | Tests de carga contra el api-gateway, con el perfil `carga` del compose; manda los resultados a Prometheus ([ADR-013](docs/adr/ADR-013-capacidad-y-costos.md), [tests/carga](tests/carga/README.md)) | Sin puerto | — |
| Prism (`stoplight/prism:5`) | Mock del contrato de notificaciones ([ADR-004](docs/adr/ADR-004-contrato-de-notificaciones.md)) | `NOTIFICACIONES_MOCK_PORT` (4010) | http://localhost:4010/notificaciones |

## Reglas

1. Trabajar siempre en una rama `feature/` creada desde `develop`.
2. Nunca commitear a `main` ni a `develop` directamente; todo entra por pull request con revisión humana.
3. Nunca commitear `.env` ni secretos.
4. Correr tests y lint antes de cada commit.
5. No elegir tecnologías por cuenta propia: cada dependencia nueva necesita un ADR aceptado en `docs/adr/`.
6. Actualizar `docs/` cuando un cambio afecte la arquitectura o los contratos.
7. **Las decisiones las toma el equipo, no la IA.** Un agente de código puede analizar, comparar alternativas y recomendar una, pero no decide. Ante cualquier decisión con alternativas (tecnología, diseño, seguridad, acceso, estructura del repo, alcance), presenta las opciones con sus ventajas y desventajas y espera a que un integrante elija. La decisión y quién la tomó quedan en el ADR o en la issue correspondiente. Si un agente aplicó algo sin preguntar, lo dice en el PR para que el equipo lo confirme o lo revierta.
