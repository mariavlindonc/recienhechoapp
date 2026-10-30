# ADR-011 (D10): Resiliencia

## Estado

- **Aceptado** para `api-gateway` y `notificaciones`, Entrega 2.
- **Aceptado** para `productos` con el plan que aprobó Agustín el 26/10/2026; implementado el mismo día.
- **Propuesto** para `pedidos`: se acepta al revisar su PR.

Revisión del 22/10/2026: se suma un circuit breaker en `notificaciones` hacia el servidor de correo, con `sony/gobreaker`, y un tiempo máximo de 24 h para las notificaciones pendientes. Sale de un ensayo de caída (ver "Por qué un circuit breaker también para el servidor de correo"). Implementado en la issue #33: el adaptador `internal/adaptadores/disyuntor` envuelve al remitente SMTP. Con el circuito abierto devuelve `ErrCorreoNoDisponible`, y la aplicación posterga la notificación sin gastar un intento. El disyuntor vive del lado de los adaptadores, como el de `pedidos` en ADR-010, porque la aplicación no importa tecnologías ([ADR-007](ADR-007-arquitectura-interna.md)). Falta la métrica y la alerta del estado del circuito (parte de Athina en la #33).

Implementación: `notificaciones` ya tiene `/health` con ping a MongoDB, timeout de 2 s por operación contra MongoDB y envío en segundo plano con reintentos. El api-gateway ya tiene el proxy con timeout de 3 s, `503 no_disponible` y el límite de uso. Falta el balanceo con chequeos de `/vivo` ([ADR-012](ADR-012-balanceo-de-carga.md)).

## Contexto

El enunciado pide identificar las dependencias de cada servicio, definir qué pasa cuando una falla o se pone lenta, implementar al menos un mecanismo de protección y demostrarlo con una caída provocada registrada en el `POSTMORTEM.md`. RNF4 pide que si inventario o notificaciones fallan, el sistema siga tomando pedidos.

## Decisión

**Dependencias y comportamiento ante fallas:**

| Servicio | Dependencia | Si falla o se pone lenta | Mecanismo |
|---|---|---|---|
| api-gateway | notificaciones | `503 no_disponible` en `/notificaciones*`; las demás rutas siguen | Timeout de 3 s; balanceo con health checks ([ADR-012](ADR-012-balanceo-de-carga.md)) |
| api-gateway | usuarios, productos, pedidos | `503` en las rutas de ese servicio | Timeout de 3 s |
| notificaciones | MongoDB | `POST` y `GET` responden `503`; nada se acepta sin guardarse | Timeout de 2 s por operación; `/health` informa la falla |
| notificaciones | Servidor de correo (SMTP) | El `POST` sigue respondiendo `202`; las notificaciones quedan `pendiente` | Envío en segundo plano con timeout de conexión de 2 s y de 10 s por envío; tras el primer intento, 3 reintentos con espera exponencial (30, 60 y 120 s) y después `fallida`; si el servidor rechaza el mensaje o el destinatario de forma definitiva (5xx), pasa a `fallida` sin reintentos. Circuit breaker (`sony/gobreaker`): con 5 fallos transitorios seguidos deja de enviar 30 s, sin gastar los reintentos de las pendientes; una pendiente que no se pudo enviar en 24 h pasa a `fallida`. Alerta sobre el estado del circuito ([ADR-006](ADR-006-observabilidad.md)) |
| pedidos | productos | No se crea el pedido: "no pudimos tomar el pedido, probá en un rato" | Timeout de 1 s y 2 reintentos ([ADR-003](ADR-003-comunicacion-entre-servicios.md)) |
| pedidos | inventario (otro grupo) | El pedido queda `pendiente_confirmacion` | Timeout de 1,5 s y circuit breaker ([ADR-010](ADR-010-consumo-de-inventario.md)) |
| pedidos | RabbitMQ | El cambio de estado se guarda igual; el evento sale cuando vuelve | Bandeja de salida (outbox, [ADR-015](ADR-015-consistencia-y-concurrencia.md)) |
| pedidos | MongoDB | No se toman pedidos (`503`) | Timeout; no hay degradación posible sin perder la garantía de cupo |
| productos | Redis | Se lee de MySQL, más lento | Timeout de 50 ms y sin reintentos ([ADR-009](ADR-009-cache.md)); no frena el arranque |
| productos | Meilisearch | `GET /productos/busqueda` responde `503 busqueda_no_disponible`; el catálogo por categoría sigue. Los cambios esperan en la bandeja y se aplican solos al volver | Outbox del índice ([ADR-008](ADR-008-busqueda.md)); 2 s por búsqueda; no frena el arranque |
| productos | MySQL | `503 no_disponible`; `/health` informa la falla | Timeout de 2 s por operación (en el DSN); el servicio no arranca sin MySQL |
| usuarios | MySQL | Registro, inicio de sesión y perfil responden `503 no_disponible`; `/health` informa la falla. Las sesiones abiertas siguen andando hasta que vence el token, porque el gateway no consulta a `usuarios` ([ADR-016](ADR-016-autenticacion.md)) | Timeout de 2 s por operación (en el DSN); el servicio no arranca sin MySQL |

**Mecanismos:**

- **Timeouts en toda llamada saliente**, sin excepción. Una dependencia lenta no puede dejar solicitudes colgadas. Los valores de la tabla son los ya definidos; donde dice solo «Timeout», el valor se fija al implementar el servicio.
- **Reintentos con espera exponencial** solo en operaciones idempotentes y errores transitorios ([ADR-003](ADR-003-comunicacion-entre-servicios.md)).
- **Circuit breaker** (`sony/gobreaker`) donde la dependencia es externa y no la controlamos. En los dos casos abre con 5 fallas seguidas, espera 30 s y deja pasar una llamada de prueba, y su estado se expone como métrica:
  - `pedidos` → inventario del otro grupo ([ADR-010](ADR-010-consumo-de-inventario.md));
  - `notificaciones` → servidor de correo. Mientras el circuito está abierto, el proceso de envío no toma notificaciones: las pendientes esperan sin gastar sus reintentos, porque no fallaron ellas sino el servidor. Un rechazo definitivo (5xx al destinatario) no cuenta como falla: el servidor respondió.
- **Trabajo en segundo plano** donde el usuario no necesita esperar: envío de mails, reconfirmación de pedidos, actualización del índice.
- **Health checks:** cada servicio expone `/health`, que verifica sus dependencias obligatorias (por ejemplo, `notificaciones` hace ping a MongoDB). Para sacar instancias caídas del balanceo, el gateway usa otro chequeo más liviano, `/vivo`, que no mira dependencias ([ADR-012](ADR-012-balanceo-de-carga.md)).
- **Límite de uso** de `notificaciones`: 60 solicitudes por minuto por grupo (todas las claves de un grupo suman), para que un grupo consumidor no tumbe el servicio. Lo aplica el api-gateway, que ve todas las solicitudes antes de repartirlas, así que es exacto con varias instancias ([ADR-012](ADR-012-balanceo-de-carga.md)).

**Por qué un circuit breaker también para el servidor de correo (revisión del 22/10/2026).** En un ensayo con Mailpit apagado y ~95 notificaciones pendientes:

- Cada intento fallido tardaba ~3,2 s en fallar (con un servidor real que no contesta, hasta los 10 s del timeout).
- El proceso de envío manda de a una, así que una vuelta por la cola tardaba ~5 minutos.
- Las primeras notificaciones recién quedaban `fallida` a los 15–20 minutos, y la alerta, que contaba fallidas, se disparaba con ese retraso.

El diseño anterior tenía dos problemas que crecían con la cola:

- El costo de una caída era *pendientes × tiempo de cada fallo*, y el servicio seguía golpeando un servidor que ya se sabía caído.
- La detección dependía de que cada notificación agotara sus reintentos, cuando el problema ya era evidente en los primeros fallos.

Con el circuit breaker, una caída cuesta 5 fallos (unos 15 s) y después el servicio espera. La detección mira el estado del circuito y no depende del tamaño de la cola. Cuando el servidor vuelve, la prueba del estado semiabierto cierra el circuito en menos de 30 s. El timeout de conexión de 2 s abarata cada fallo, pero por sí solo no resuelve que el costo crezca con la cola.

Como mientras el circuito está abierto no se gastan reintentos, una notificación podría quedar `pendiente` indefinidamente si el correo está caído mucho tiempo. Por eso, una pendiente que lleva **24 horas** sin poder enviarse pasa a `fallida`, con un motivo que lo explica. Es el mismo plazo que se recuerdan las claves de idempotencia.

**Por qué `sony/gobreaker` y no código propio.**

- **Una sola forma de hacer circuit breaker en el sistema.** `gobreaker` ya estaba elegido para `pedidos`. Usar la misma librería en notificaciones evita dos implementaciones del mismo patrón, que habría que mantener y explicar por separado.
- **Cubre exactamente el diseño**, sin código extra:
  - `ReadyToTrip`: abrir con 5 fallos seguidos;
  - `Timeout`: 30 s abierto;
  - `MaxRequests: 1`: una sola prueba en semiabierto;
  - `IsSuccessful`: que un rechazo 5xx no cuente como falla;
  - `OnStateChange`: publicar el estado en las métricas y en los logs;
  - `State()`: no tomar pendientes con el circuito abierto.
- **Probada:** es la implementación más usada en Go, mantenida por Sony, con tests de concurrencia y de transiciones entre estados. Los errores típicos de escribirlo a mano son de concurrencia y de tiempos, por ejemplo dos pruebas a la vez en semiabierto.
- **Costo bajo:** v2.4.0 (enero de 2026), sin dependencias en tiempo de ejecución, y ya estaba aceptada en este ADR.

**Caídas previstas para el POSTMORTEM (Entrega 2):**

1. **Servidor de correo caído** (`docker compose stop mailpit`):
   - El `POST /notificaciones` sigue respondiendo `202` y las notificaciones quedan `pendiente`.
   - El circuito se abre después de 5 fallos seguidos y desde ahí hay como mucho un intento de prueba cada ~35 s (los 30 s del circuito más hasta 5 s del sondeo). Las pendientes no gastan reintentos. La alerta del estado del circuito se dispara en unos 2 minutos, sin importar cuántas pendientes haya (cuando esté la métrica).
   - Al volver Mailpit, la prueba del estado semiabierto cierra el circuito y salen todas las pendientes.
   - **Medido el 22/10/2026**, con 60 notificaciones (el límite de 60 por minuto rechazó con `429` las 40 restantes de una ráfaga de 100):
     - el circuito se abrió a los ~10 s;
     - hubo 6 intentos SMTP en total (5 que lo abrieron y 1 prueba), contra uno cada ~3 s sin el disyuntor;
     - al volver Mailpit, las 59 pendientes salieron en 32 s y ninguna quedó `fallida`.

     En el ensayo anterior, sin el disyuntor, la alerta tardaba 15 a 20 minutos y la cola, varios minutos más.
2. **Una instancia de notificaciones caída** (`docker compose stop notificaciones-1`): el gateway la saca del balanceo y todo el tráfico va a la otra; con las dos caídas, `503 no_disponible`.
   - **Medido el 29/10/2026**, con un `POST /notificaciones` cada 0,25 s:
     - con `notificaciones-1` detenida, 19 de 20 respondieron `202`; el único `503` fue el reenvío que detectó la caída y la sacó del balanceo;
     - con `docker compose start notificaciones-1`, volvió en el chequeo siguiente (menos de 8 s) y otra vez recibió tráfico;
     - con las dos detenidas, las 4 solicitudes respondieron `503 no_disponible`.

Para la presentación se suma la caída del inventario del proveedor (circuit breaker y `pendiente_confirmacion`).

## Alternativas consideradas

- **Reintentar todo, siempre.** Fácil, pero multiplica la carga sobre una dependencia que ya está mal y puede duplicar operaciones no idempotentes.
- **Circuit breaker propio** (unas 50–80 líneas) en lugar de `sony/gobreaker`. No suma dependencias y se puede explicar línea por línea, pero deja dos implementaciones del mismo patrón en el sistema y hay que probar a mano la concurrencia y los tiempos.
- **Varios envíos de mails en paralelo** para vaciar la cola más rápido. Divide el tiempo de cada vuelta, pero golpea más fuerte a un servidor caído, no mejora la detección y Gmail limita los envíos.
- **Detectar la caída del correo contando notificaciones `fallida`** (el diseño anterior). Solo funciona con pocas pendientes: el tiempo de detección crece con la cola.
- **Circuit breaker en todas las llamadas.** Más protección, pero más estados que entender y configurar. Se pone donde la dependencia es externa y no la controlamos.
- **Bulkheads (pools separados por dependencia).** Útiles con mucho tráfico; con nuestro volumen alcanza con timeouts.
- **Service mesh (Istio, Linkerd).** Resuelve reintentos y circuit breakers fuera del código, pero exige Kubernetes.

## Consecuencias

- Cada servicio tiene que tener configurados sus timeouts y exponer `/health`.
- `sony/gobreaker` entra como dependencia de `pedidos` y de `notificaciones`.
- Con el correo caído, una notificación puede quedar `pendiente` hasta 24 horas antes de pasar a `fallida`; el consumidor lo ve en el `GET`.
- Cada instancia de notificaciones tiene su propio circuito (ADR-012): con dos instancias, cada una detecta la caída por su cuenta.
- Hay estados intermedios visibles para el usuario (`pendiente`, `pendiente_confirmacion`) que hay que explicar en la interfaz.
- Los valores de timeout y del circuit breaker son iniciales y se ajustan con los tests de carga ([ADR-013](ADR-013-capacidad-y-costos.md)).
