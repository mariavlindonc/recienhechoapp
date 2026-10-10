# Recién hecho

E-commerce de pastelería a pedido: los compradores encargan productos para ahora, para una fecha futura o a medida, y la pastelería organiza su producción según la capacidad que tiene cada día.

Trabajo Práctico Integrador de Arquitectura de Software 2026, Facultad de Ingeniería (UCC).

## Integrantes

- Agustín Di Mario
- María Victoria Lindon
- Athina Terrera

## Estado

Versión 0.1.0, Entrega 1: diseño y contrato con mock. Los servicios todavía solo exponen `/health`; la funcionalidad se implementa a partir de la Entrega 2.

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

Cómo probar el mock del contrato de notificaciones: [docs/contracts](docs/contracts/README.md#probar-contra-el-mock).

Todavía no hay nada desplegado. La capacidad de notificaciones se va a publicar en una URL pública antes de la presentación, y el link va a estar acá.

## Documentación

| Documento | Contenido |
|---|---|
| [SPEC.md](SPEC.md) | Alcance: actores, funcionalidades, reglas de negocio y criterios de aceptación |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Servicios, datos, comunicación y diagramas |
| [docs/adr/](docs/adr/) | Decisiones de arquitectura |
| [docs/contracts/](docs/contracts/README.md) | Contrato publicado (notificaciones) y consumido (inventario) |
| [docs/POSTMORTEM.md](docs/POSTMORTEM.md) | Informe de la caída controlada (pendiente) |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Ramas, commits y pull requests |
| [AGENTS.md](AGENTS.md) | Comandos, convenciones y reglas (también para agentes de código) |

### Decisiones

| Decisión | ADR | Estado |
|---|---|---|
| D1 Límites de los servicios | [ADR-001](docs/adr/ADR-001-limites-de-los-servicios.md) | Aceptado |
| D3 Persistencia | [ADR-002](docs/adr/ADR-002-persistencia.md) | Propuesto |
| D5 Comunicación entre servicios | [ADR-003](docs/adr/ADR-003-comunicacion-entre-servicios.md) | Propuesto |
| D8 Contrato propio | [ADR-004](docs/adr/ADR-004-contrato-de-notificaciones.md) | Aceptado |
