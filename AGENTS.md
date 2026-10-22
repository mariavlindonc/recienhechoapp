# AGENTS.md

Instrucciones para agentes de código (Claude Code, Antigravity, Codex, OpenCode) y personas. `CLAUDE.md` y `GEMINI.md` solo remiten a este archivo.

## Proyecto

**Recién hecho**: e-commerce de pastelería a pedido. Microservicios en Go con Gin, orquestados con Docker Compose. Requisitos en [SPEC.md](docs/SPEC.md).

```
backend/    api-gateway, usuarios, productos, pedidos, notificaciones (cada uno con su go.mod y Dockerfile)
frontend/   pendiente
docs/       arquitectura, ADRs, contratos, postmortems
observabilidad/  configuración de Prometheus (lectura de métricas y alertas) y de Grafana (fuentes y tablero)
```

## Comandos

```bash
cp .env.example .env                 # una sola vez
docker compose up --build -d         # levantar
docker compose down                  # bajar

# Tests y lint, desde la raíz (en Windows, con Git Bash). gofmt no tiene que listar nada.
for servicio in backend/*/; do (cd "$servicio" && go test ./... && go vet ./...) || break; done; gofmt -l backend
```

## Convenciones

- Código con `gofmt` y `go vet` limpio.
- Identificadores, comentarios y mensajes en español, sin variables de una letra.
- Errores manejados y devueltos con contexto (`fmt.Errorf("...: %w", err)`).
- Configuración por variables de entorno; nada sensible en el código.
- Un servicio no importa código de otro.
- Commits con [Conventional Commits](https://www.conventionalcommits.org/es/) en español: `feat(productos): agrega listado de catálogo`.
- Ramas con git flow; detalle en [CONTRIBUTING.md](docs/CONTRIBUTING.md).

## Infraestructura

Cada dependencia que se agregue se lista acá con su uso, puerto y URL local. Las demás bases, la caché y el broker están propuestos en [ADR-002](docs/adr/ADR-002-persistencia.md) y [ADR-003](docs/adr/ADR-003-comunicacion-entre-servicios.md), pero todavía no están en el compose.

Las herramientas de desarrollo (MongoDB, Mailpit, Jaeger, Prometheus y Grafana) publican su puerto solo en `127.0.0.1`: se usan desde la propia computadora y no quedan expuestas a la red local ([ADR-006](docs/adr/ADR-006-observabilidad.md)).

| Dependencia | Uso | Puerto | URL local |
|---|---|---|---|
| MongoDB (`mongo:8`) | Base de `notificaciones` ([ADR-002](docs/adr/ADR-002-persistencia.md)) | `NOTIFICACIONES_MONGO_PORT` (27017) | `mongodb://localhost:27017` (usuario y clave en `.env`) |
| Mailpit (`axllent/mailpit`) | Buzón falso para desarrollo ([ADR-005](docs/adr/ADR-005-envio-de-email.md)) | `MAILPIT_SMTP_PORT` (1025) y `MAILPIT_WEB_PORT` (8025) | http://localhost:8025 |
| Prometheus (`prom/prometheus:v3.15.0`) | Lee `/metrics` de los servicios (puerto interno 9464) y evalúa las alertas ([ADR-006](docs/adr/ADR-006-observabilidad.md)) | `PROMETHEUS_WEB_PORT` (9090) | http://localhost:9090 |
| Grafana (`grafana/grafana:13.2.3`) | Tablero de métricas y acceso a las trazas ([ADR-006](docs/adr/ADR-006-observabilidad.md)) | `GRAFANA_WEB_PORT` (3000) | http://localhost:3000 (sin usuario, solo lectura; admin con `GRAFANA_ADMIN_CLAVE`) |
| Jaeger (`jaegertracing/jaeger:2.22.0`) | Trazas de api-gateway y notificaciones por OTLP ([ADR-006](docs/adr/ADR-006-observabilidad.md)) | `JAEGER_WEB_PORT` (16686) | http://localhost:16686 |
| Prism (`stoplight/prism:5`) | Mock del contrato de notificaciones ([ADR-004](docs/adr/ADR-004-contrato-de-notificaciones.md)) | `NOTIFICACIONES_MOCK_PORT` (4010) | http://localhost:4010/notificaciones |

## Reglas

1. Trabajar siempre en una rama `feature/` creada desde `develop`.
2. Nunca commitear a `main` ni a `develop` directamente; todo entra por pull request con revisión humana.
3. Nunca commitear `.env` ni secretos.
4. Correr tests y lint antes de cada commit.
5. No elegir tecnologías por cuenta propia: cada dependencia nueva necesita un ADR aceptado en `docs/adr/`.
6. Actualizar `docs/` cuando un cambio afecte la arquitectura o los contratos.
7. **Las decisiones las toma el equipo, no la IA.** Un agente de código puede analizar, comparar alternativas y recomendar una, pero no decide. Ante cualquier decisión con alternativas (tecnología, diseño, seguridad, acceso, estructura del repo, alcance), presenta las opciones con sus ventajas y desventajas y espera a que un integrante elija. La decisión y quién la tomó quedan en el ADR o en la issue correspondiente. Si un agente aplicó algo sin preguntar, lo dice en el PR para que el equipo lo confirme o lo revierta.
