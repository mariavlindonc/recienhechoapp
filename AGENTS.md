# AGENTS.md

Instrucciones para agentes de código (Claude Code, Antigravity, Codex, OpenCode) y personas. `CLAUDE.md` y `GEMINI.md` solo remiten a este archivo.

## Proyecto

**Recién hecho**: e-commerce de pastelería a pedido. Microservicios en Go con Gin, orquestados con Docker Compose. Requisitos en [SPEC.md](SPEC.md).

```
services/   api-gateway, usuarios, productos, pedidos, notificaciones (cada uno con su go.mod y Dockerfile)
frontend/   pendiente
docs/       arquitectura, ADRs, contratos, postmortems
```

## Comandos

```bash
cp .env.example .env                 # una sola vez
docker compose up --build -d         # levantar
docker compose down                  # bajar

# Tests y lint, desde la raíz (en Windows, con Git Bash). gofmt no tiene que listar nada.
for servicio in services/*/; do (cd "$servicio" && go test ./... && go vet ./...) || break; done; gofmt -l services
```

## Convenciones

- Código con `gofmt` y `go vet` limpio.
- Identificadores, comentarios y mensajes en español, sin variables de una letra.
- Errores manejados y devueltos con contexto (`fmt.Errorf("...: %w", err)`).
- Configuración por variables de entorno; nada sensible en el código.
- Un servicio no importa código de otro.
- Commits con [Conventional Commits](https://www.conventionalcommits.org/es/) en español: `feat(productos): agrega listado de catálogo`.
- Ramas con git flow; detalle en [CONTRIBUTING.md](CONTRIBUTING.md).

## Infraestructura

Cada dependencia que se agregue se lista acá con su uso, puerto y URL local. Las bases, la caché y el broker están propuestos en [ADR-002](docs/adr/ADR-002-persistencia.md) y [ADR-003](docs/adr/ADR-003-comunicacion-entre-servicios.md), pero todavía no están en el compose.

| Dependencia | Uso | Puerto | URL local |
|---|---|---|---|
| Prism (`stoplight/prism:5`) | Mock del contrato de notificaciones ([ADR-004](docs/adr/ADR-004-contrato-de-notificaciones.md)) | `NOTIFICACIONES_MOCK_PORT` (4010) | http://localhost:4010/v1/notificaciones |

## Reglas

1. Trabajar siempre en una rama `feature/` creada desde `develop`.
2. Nunca commitear a `main` ni a `develop` directamente; todo entra por pull request con revisión humana.
3. Nunca commitear `.env` ni secretos.
4. Correr tests y lint antes de cada commit.
5. No elegir tecnologías por cuenta propia: cada dependencia nueva necesita un ADR aceptado en `docs/adr/`.
6. Actualizar `docs/` cuando un cambio afecte la arquitectura o los contratos.
