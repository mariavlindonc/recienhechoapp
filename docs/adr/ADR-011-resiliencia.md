# ADR-011 (D10): Resiliencia

## Estado

- **Aceptado** para `api-gateway` y `notificaciones`, Entrega 2.
- **Propuesto** para `pedidos` y `productos`: se acepta al implementar cada servicio.

Implementación: `notificaciones` ya tiene `/health` con ping a MongoDB, timeout de 2 s por operación contra MongoDB, límite de uso y envío en segundo plano con reintentos. El api-gateway ya tiene el proxy con timeout de 3 s y `503 no_disponible`. Falta el balanceo con chequeos de `/vivo` ([ADR-012](ADR-012-balanceo-de-carga.md)).

## Contexto

El enunciado pide identificar las dependencias de cada servicio, definir qué pasa cuando una falla o se pone lenta, implementar al menos un mecanismo de protección y demostrarlo con una caída provocada registrada en el `POSTMORTEM.md`. RNF4 pide que si inventario o notificaciones fallan, el sistema siga tomando pedidos.

## Decisión

**Dependencias y comportamiento ante fallas:**

| Servicio | Dependencia | Si falla o se pone lenta | Mecanismo |
|---|---|---|---|
| api-gateway | notificaciones | `503 no_disponible` en `/notificaciones*`; las demás rutas siguen | Timeout de 3 s; balanceo con health checks ([ADR-012](ADR-012-balanceo-de-carga.md)) |
| api-gateway | usuarios, productos, pedidos | `503` en las rutas de ese servicio | Timeout de 3 s |
| notificaciones | MongoDB | `POST` y `GET` responden `503`; nada se acepta sin guardarse | Timeout de 2 s por operación; `/health` informa la falla |
| notificaciones | Servidor de correo (SMTP) | El `POST` sigue respondiendo `202`; las notificaciones quedan `pendiente` | Envío en segundo plano con timeout de 10 s por envío; tras el primer intento, 3 reintentos con espera exponencial (30, 60 y 120 s) y después `fallida`; si el servidor rechaza el mensaje o el destinatario de forma definitiva (5xx), pasa a `fallida` sin reintentos; alerta de [ADR-006](ADR-006-observabilidad.md) |
| pedidos | productos | No se crea el pedido: "no pudimos tomar el pedido, probá en un rato" | Timeout de 1 s y 2 reintentos ([ADR-003](ADR-003-comunicacion-entre-servicios.md)) |
| pedidos | inventario (otro grupo) | El pedido queda `pendiente_confirmacion` | Timeout de 1,5 s y circuit breaker ([ADR-010](ADR-010-consumo-de-inventario.md)) |
| pedidos | RabbitMQ | El cambio de estado se guarda igual; el evento sale cuando vuelve | Outbox (D4) |
| pedidos | MongoDB | No se toman pedidos (`503`) | Timeout; no hay degradación posible sin perder la garantía de cupo |
| productos | Redis | Se lee de MySQL, más lento | Timeout de 50 ms ([ADR-009](ADR-009-cache.md)) |
| productos | Meilisearch | Sin búsqueda de texto; queda el listado por categoría | Outbox del índice ([ADR-008](ADR-008-busqueda.md)) |
| productos | MySQL | `503` | Timeout |

**Mecanismos:**

- **Timeouts en toda llamada saliente**, sin excepción. Una dependencia lenta no puede dejar solicitudes colgadas. Los valores de la tabla son los ya definidos; donde dice solo «Timeout», el valor se fija al implementar el servicio.
- **Reintentos con espera exponencial** solo en operaciones idempotentes y errores transitorios ([ADR-003](ADR-003-comunicacion-entre-servicios.md)).
- **Circuit breaker** (`sony/gobreaker`) en las llamadas de `pedidos` al inventario: abre con 5 fallas seguidas, espera 30 s y deja pasar una llamada de prueba. Su estado se expone como métrica.
- **Trabajo en segundo plano** donde el usuario no necesita esperar: envío de mails, reconfirmación de pedidos, actualización del índice.
- **Health checks:** cada servicio expone `/health`, que verifica sus dependencias obligatorias (por ejemplo, `notificaciones` hace ping a MongoDB). Para sacar instancias caídas del balanceo, el gateway usa otro chequeo más liviano, `/vivo`, que no mira dependencias ([ADR-012](ADR-012-balanceo-de-carga.md)).
- **Límite de uso** en `notificaciones`: 60 solicitudes por minuto por grupo (todas las claves de un grupo suman), para que un grupo consumidor no tumbe el servicio. El contador está en la memoria de cada instancia ([ADR-012](ADR-012-balanceo-de-carga.md)).

**Caídas previstas para el POSTMORTEM (Entrega 2):**

1. **Servidor de correo caído** (`docker compose stop mailpit`): el `POST /notificaciones` sigue respondiendo `202`, las notificaciones quedan `pendiente`, se reintentan y terminan `fallida` a los 3,5 minutos del primer intento (30 + 60 + 120 s de espera, más hasta 5 s del sondeo); con más de 5 notificaciones `fallida` se dispara la alerta, así que la prueba tiene que mandar al menos 6. Al volver Mailpit, las nuevas se envían normalmente.
2. **Una instancia de notificaciones caída** (`docker compose stop notificaciones-1`): el gateway la saca del balanceo y todo el tráfico va a la otra; con las dos caídas, `503 no_disponible`.

Para la presentación se suma la caída del inventario del proveedor (circuit breaker y `pendiente_confirmacion`).

## Alternativas consideradas

- **Reintentar todo, siempre.** Fácil, pero multiplica la carga sobre una dependencia que ya está mal y puede duplicar operaciones no idempotentes.
- **Circuit breaker en todas las llamadas.** Más protección, pero más estados que entender y configurar. Se pone donde la dependencia es externa y no la controlamos.
- **Bulkheads (pools separados por dependencia).** Útiles con mucho tráfico; con nuestro volumen alcanza con timeouts.
- **Service mesh (Istio, Linkerd).** Resuelve reintentos y circuit breakers fuera del código, pero exige Kubernetes.

## Consecuencias

- Cada servicio tiene que tener configurados sus timeouts y exponer `/health`.
- `sony/gobreaker` entra como dependencia de `pedidos`.
- Hay estados intermedios visibles para el usuario (`pendiente`, `pendiente_confirmacion`) que hay que explicar en la interfaz.
- Los valores de timeout y del circuit breaker son iniciales y se ajustan con los tests de carga ([ADR-013](ADR-013-capacidad-y-costos.md)).
