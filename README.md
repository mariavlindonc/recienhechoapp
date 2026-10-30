# Recién hecho

E-commerce de pastelería a pedido: los compradores encargan productos para ahora, para una fecha futura o a medida, y la pastelería organiza su producción según la capacidad que tiene cada día.

Trabajo Práctico Integrador de Arquitectura de Software 2026, Facultad de Ingeniería (UCC).

## Integrantes

- Agustín Di Mario
- María Victoria Lindon
- Athina Terrera

## Objetivo

Que un comprador pueda encargar sin llamar a la pastelería, y que la pastelería no acepte más de lo que puede producir.

## Flujo principal

1. El comprador busca en el catálogo y elige productos.
2. Encarga para una fecha: el sistema reserva el cupo de producción de ese día. Si no queda cupo, ofrece otra fecha.
3. El sistema reserva los insumos de la receta en el inventario (capacidad de otro grupo) y confirma el pedido.
4. El vendedor ve el pedido en su calendario de producción y avanza su estado hasta "listo".
5. El comprador recibe un email en cada cambio de estado.

## Arquitectura

Cuatro microservicios en Go (Gin) detrás de un API Gateway: **usuarios**, **productos**, **pedidos** y **notificaciones**, más un frontend en React. Publicamos la capacidad de **notificaciones** para que la use otro grupo, y consumimos la de **inventario** que publica otro grupo. Diagramas y detalle en [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

Última versión publicada: 0.2.0 (Entrega 2). `develop` ya tiene todos los servicios, el frontend, observabilidad, balanceo de carga, tests de carga y caídas controladas. Todavía no hay nada desplegado; la demo va a ir en un servidor propio con Cloudflare Tunnel ([ADR-013](docs/adr/ADR-013-capacidad-y-costos.md)).

## Cómo levantarlo

Requisitos: Docker con Compose.

```bash
cp .env.example .env && docker compose up --build -d
```

Para bajarlo: `docker compose down`.

| Qué | URL local |
|---|---|
| Frontend | http://localhost:5173 |
| api-gateway | http://localhost:8080 |
| Grafana (métricas) | http://localhost:3000 |
| Jaeger (trazas) | http://localhost:16686 |
| Mailpit (buzón falso) | http://localhost:8025 |

El resto de los puertos está en [AGENTS.md](AGENTS.md#infraestructura).

Tests y lint (en Windows, con Git Bash):

```bash
for servicio in backend/*/; do (cd "$servicio" && go test -race ./... && go vet ./...) || break; done; gofmt -l backend
(cd frontend && bunx tsc && bun test)
```

## Documentación

| Documento | Contenido |
|---|---|
| [docs/SPEC.md](docs/SPEC.md) | Alcance: actores, funcionalidades, reglas de negocio y criterios de aceptación |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Servicios, datos, comunicación y diagramas |
| [docs/adr/](docs/adr/) | Decisiones de arquitectura |
| [docs/contracts/](docs/contracts/README.md) | Contrato publicado (notificaciones) y consumido (inventario) |
| [docs/POSTMORTEM.md](docs/POSTMORTEM.md) | Informes de las caídas controladas |
| [tests/carga/](tests/carga/README.md) | Tests de carga con k6 y sus resultados |
| [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md) | Flujo de ramas, commits y pull requests |
| [AGENTS.md](AGENTS.md) | Convenciones, infraestructura y reglas para quien trabaje en el código |
