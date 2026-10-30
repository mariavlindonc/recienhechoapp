# Recién hecho

E-commerce de pastelería a pedido: los compradores encargan productos para ahora, para una fecha futura o a medida, y la pastelería organiza su producción según la capacidad que tiene cada día.

Trabajo Práctico Integrador de Arquitectura de Software 2026, Facultad de Ingeniería (UCC).

## Integrantes

- Agustín Di Mario
- María Victoria Lindon
- Athina Terrera

## Estado

Última versión publicada: 0.2.0 (Entrega 2), con `notificaciones` funcionando de punta a punta detrás del api-gateway, con MongoDB, logs, trazas y métricas. La tabla de abajo es el estado de `develop`, que ya incluye el resto de los servicios y el frontend.

| Componente | Estado |
|---|---|
| `notificaciones` | Implementado: `POST` y `GET /notificaciones` del contrato, idempotencia, clave de API por grupo, límite de uso (lo aplica el api-gateway), envío de mails en segundo plano con reintentos, MongoDB y SMTP |
| `api-gateway` | Reenvía `/notificaciones*` a `notificaciones`; `/pedidos*` y `/capacidad*` a `pedidos`; y `/productos*`, `/categorias*` e `/imagenes*` a `productos`, con timeout de 3 s (`503 no_disponible` si no responde). Enruta `/usuarios*` a `usuarios`, verifica el JWT de `Authorization: Bearer` y arma con él los headers de identidad ([ADR-016](docs/adr/ADR-016-autenticacion.md)). Aplica el límite de 60 solicitudes por minuto por grupo de `notificaciones` ([ADR-004](docs/adr/ADR-004-contrato-de-notificaciones.md)) |
| `pedidos` | Implementado: encargar (inmediato y programado) con reserva de cupo en una transacción, idempotencia, cancelar, avanzar estados, capacidad por día, inventario falso con disyuntor y reconfirmación, eventos a RabbitMQ por outbox, y pedidos especiales con cotización que vence a las 48 h ([ADR-017](docs/adr/ADR-017-pedidos-especiales.md)) |
| `productos` | Implementado: catálogo con caché en Redis, búsqueda con Meilisearch sincronizada por outbox, ABM de categorías y productos, receta, subida de imágenes y ruta interna para `pedidos` |
| `usuarios` | Registro, inicio de sesión con token JWT y perfil, en MySQL ([ADR-016](docs/adr/ADR-016-autenticacion.md)) |
| Frontend | React, TypeScript y material-esm ([ADR-014](docs/adr/ADR-014-frontend.md)), en `localhost:5173`: catálogo con búsqueda, encargar, pedidos especiales, ingreso y registro, y las pantallas del vendedor, todo contra los servicios por el gateway. Tema "Etiqueta kraft" de la marca |
| Logs JSON con `trace_id`, trazas (Jaeger), métricas y alertas (Prometheus y Grafana) | Implementados en el api-gateway, `notificaciones`, `pedidos` y `productos` |
| Balanceo de carga | El compose levanta `notificaciones-1` y `notificaciones-2`, y el gateway reparte en round-robin entre las sanas: las chequea con `/vivo` cada 5 s y saca las que fallan ([ADR-012](docs/adr/ADR-012-balanceo-de-carga.md)) |
| Tests de carga | Escenarios de k6 en [tests/carga](tests/carga/README.md) para notificaciones, catálogo (con caché y sin caché) y pedidos. Corridos el 29/10/2026: todos los objetivos se cumplen, y el límite principal son las conexiones del gateway hacia `pedidos` ([ADR-013](docs/adr/ADR-013-capacidad-y-costos.md#resultados)) |
| Caídas controladas | Tres ensayos con su informe en [POSTMORTEM](docs/POSTMORTEM.md): una instancia de notificaciones, el servidor de correo y el inventario lento |

## Objetivo

Que un comprador pueda encargar sin llamar a la pastelería, y que la pastelería no acepte más de lo que puede producir.

## Flujo principal

1. El comprador busca en el catálogo y elige productos.
2. Encarga para una fecha: el sistema reserva el cupo de producción de ese día. Si no queda cupo, ofrece otra fecha.
3. El sistema reserva los insumos de la receta en el inventario (capacidad de otro grupo) y confirma el pedido.
4. El vendedor ve el pedido en su calendario de producción y avanza su estado hasta "listo".
5. El comprador recibe un email en cada cambio de estado.

## Arquitectura

Cuatro microservicios en Go (Gin) detrás de un API Gateway: **usuarios**, **productos**, **pedidos** y **notificaciones**. Diagramas y detalle en [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

Publicamos la capacidad de **notificaciones** para que la use otro grupo, y consumimos la de **inventario** que publica otro grupo.

## Cómo levantarlo

Requisitos: Docker con Compose.

```bash
cp .env.example .env && docker compose up --build -d
```

Levanta los cuatro servicios, el api-gateway, el frontend, las bases (MongoDB, MySQL, Redis y Meilisearch), RabbitMQ, Mailpit, el mock del contrato y la observabilidad (Jaeger, Prometheus y Grafana). Para bajarlo: `docker compose down`. Si ya tenías un `.env`, copiá de `.env.example` las variables que te falten.

| Qué | URL local |
|---|---|
| Frontend, la interfaz web (reenvía `/api` al gateway) | http://localhost:5173 |
| api-gateway, entrada del sistema (`/notificaciones`, `/pedidos`, `/capacidad`, `/productos`, `/categorias`, `/usuarios`) | http://localhost:8080 |
| Meilisearch, para probar búsquedas (clave en `.env`, `MEILI_CLAVE`) | http://localhost:7700 |
| Grafana, tablero de métricas (sin usuario, solo lectura) | http://localhost:3000 |
| Prometheus, métricas y alertas | http://localhost:9090 |
| Jaeger, trazas | http://localhost:16686 |
| Mailpit, buzón falso donde llegan los mails | http://localhost:8025 |
| Mock del contrato de notificaciones | http://localhost:4010/notificaciones |
| MongoDB de notificaciones | `mongodb://localhost:27017` (usuario y clave en `.env`) |

Probar el envío con el servicio real (la clave de desarrollo está en `.env`, variable `NOTIFICACIONES_CLAVES_API`):

```bash
curl -i -X POST http://localhost:8080/notificaciones   -H "X-API-Key: una-clave-larga-de-desarrollo"   -H "Idempotency-Key: prueba-1"   -H "Content-Type: application/json"   -d '{"canal":"email","destinatario":"cliente@ejemplo.com","asunto":"Hola","cuerpo":"Prueba."}'
```

El `GET /notificaciones/{id}` pasa de `pendiente` a `enviada` en unos segundos y el mail aparece en Mailpit. Contrato completo y prueba contra el mock: [docs/contracts](docs/contracts/README.md#probar-contra-el-mock).

Tests y lint (en Windows, con Git Bash):

```bash
for servicio in backend/*/; do (cd "$servicio" && go test -race ./... && go vet ./...) || break; done; gofmt -l backend
(cd frontend && bunx tsc && bun test)
```

Los tests de integración corren solo si existe la variable de su base: `MONGO_URI_TEST`, `MYSQL_DSN_TEST`, `REDIS_URL_TEST` y `MEILI_URL_TEST` (cada test dice cómo levantar la base en Docker). Ver [AGENTS.md](AGENTS.md) y `.claude/skills/run-recienhechoapp/`.

Los tests de carga (k6) y cómo correrlos están en [tests/carga](tests/carga/README.md).

Operación de `productos`:

```bash
# Reconstruir el índice de búsqueda desde MySQL, sin dejar la búsqueda vacía (ADR-008)
docker compose exec productos /servicio reindexar

# Medir el catálogo sin caché (ADR-009): CACHE_HABILITADA=false en .env y
docker compose up -d productos
```

### Despliegue

Todavía no hay nada desplegado. La capacidad de notificaciones se va a publicar en una URL pública antes de la presentación, y el link va a estar acá. Las opciones están en [ADR-013](docs/adr/ADR-013-capacidad-y-costos.md); todavía no se eligió ninguna.

## Documentación

| Documento | Contenido |
|---|---|
| [docs/SPEC.md](docs/SPEC.md) | Alcance: actores, funcionalidades, reglas de negocio y criterios de aceptación |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Servicios, datos, comunicación y diagramas |
| [docs/adr/](docs/adr/) | Decisiones de arquitectura |
| [docs/contracts/](docs/contracts/README.md) | Contrato publicado (notificaciones) y consumido (inventario) |
| [docs/POSTMORTEM.md](docs/POSTMORTEM.md) | Informes de las caídas controladas |
| [tests/carga/](tests/carga/README.md) | Tests de carga con k6 y sus resultados crudos |
| [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md) | Flujo de ramas, commits y pull requests |
| [AGENTS.md](AGENTS.md) | Convenciones y reglas para quien trabaje en el código, personas o agentes |

### Decisiones

| Decisión | ADR | Estado |
|---|---|---|
| D1 Límites de los servicios | [ADR-001](docs/adr/ADR-001-limites-de-los-servicios.md) | Aceptado |
| D3 Persistencia | [ADR-002](docs/adr/ADR-002-persistencia.md) | Aceptado para notificaciones; propuesto para el resto |
| D5 Comunicación entre servicios | [ADR-003](docs/adr/ADR-003-comunicacion-entre-servicios.md) | Aceptado para el gateway; propuesto para el resto |
| D8 Contrato propio | [ADR-004](docs/adr/ADR-004-contrato-de-notificaciones.md) | Aceptado |
| Canal y servidor de envío | [ADR-005](docs/adr/ADR-005-envio-de-email.md) | Aceptado |
| D11 Observabilidad | [ADR-006](docs/adr/ADR-006-observabilidad.md) | Aceptado (versión inicial) |
| D2 Arquitectura interna | [ADR-007](docs/adr/ADR-007-arquitectura-interna.md) | Aceptado para notificaciones y gateway; propuesto para el resto |
| D6 Búsqueda | [ADR-008](docs/adr/ADR-008-busqueda.md) | Aceptado |
| D7 Caché | [ADR-009](docs/adr/ADR-009-cache.md) | Aceptado; medida con y sin caché |
| D9 Consumo del proveedor | [ADR-010](docs/adr/ADR-010-consumo-de-inventario.md) | Propuesto |
| D10 Resiliencia | [ADR-011](docs/adr/ADR-011-resiliencia.md) | Aceptado para gateway y notificaciones; propuesto para el resto |
| D12 Balanceo de carga | [ADR-012](docs/adr/ADR-012-balanceo-de-carga.md) | Aceptado e implementado |
| D13 Capacidad y costos | [ADR-013](docs/adr/ADR-013-capacidad-y-costos.md) | Propuesto; tests de carga corridos, falta elegir dónde desplegar |
| Frontend | [ADR-014](docs/adr/ADR-014-frontend.md) | Propuesto (stack, tema y animaciones elegidos por Agustín) |
| D4 Consistencia y concurrencia | [ADR-015](docs/adr/ADR-015-consistencia-y-concurrencia.md) | Aceptado para las transacciones de MongoDB; propuesto para el resto |
| Autenticación | [ADR-016](docs/adr/ADR-016-autenticacion.md) | Aceptado |
| Pedidos especiales (C6, VE5, R8) | [ADR-017](docs/adr/ADR-017-pedidos-especiales.md) | Propuesto; implementado, a confirmar por el equipo |
