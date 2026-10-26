# ADR-003 (D5): Comunicación entre servicios

## Estado

- **Aceptado** para la comunicación síncrona del api-gateway, Entrega 2 (validado con el proxy de `/notificaciones*`).
- **Aceptado** para el resto (llamadas de `pedidos` y mensajería con RabbitMQ) por Agustín el 26/10/2026; implementado con `pedidos`.

## Contexto

El enunciado pide comunicación síncrona (con timeout, manejo de errores, respuestas tardías o ausentes y reintentos) y asíncrona con mensajería (publicar y consumir un evento de dominio y tratar los mensajes que fallan).

En Recién hecho hay dos tipos de interacción:

- Las que necesitan la respuesta para seguir: crear un pedido necesita el precio actual del producto y saber si hay insumos.
- Las que pueden pasar después: avisarle al comprador que su pedido cambió de estado no tiene que frenar el cambio de estado.

## Decisión

**Síncrono: HTTP/JSON.**

| Llamada | Timeout | Reintentos |
|---|---|---|
| api-gateway → servicios internos | 3 s | No (lo decide el cliente) |
| pedidos → productos (precio y receta) | 1 s | 2, con espera exponencial (100 ms, 200 ms) |
| pedidos → inventario (otro grupo) | 1,5 s | No: si no contesta, el pedido queda `pendiente_confirmacion` y se confirma después (detalle en D9) |

- Solo se reintentan operaciones idempotentes (GET, o POST con `Idempotency-Key`) y errores transitorios (timeout, 502, 503, 504). Los 4xx no se reintentan.
- Una respuesta que llega después del timeout se descarta.
- Si `productos` no responde, el pedido no se crea y el comprador ve "no pudimos tomar el pedido, probá en un rato". No se usan precios de caché para cobrar.
- Los mecanismos de protección (circuit breaker, degradación) se definen en D10.

**Asíncrono: RabbitMQ** (el broker que se usa en la materia).

- `pedidos` publica en un exchange `topic` llamado `pedidos`, con routing keys `pedido.confirmado` y `pedido.estado_cambiado`.
- `notificaciones` consume con su propia cola y le avisa al comprador.
- Mensajes persistentes y confirmación manual (ack) después de procesar.
- Si un mensaje falla, se reintenta hasta 3 veces. Después va a una cola de mensajes fallidos (DLQ), `notificaciones.fallidos`, para revisarlo a mano. Un mensaje que no se puede leer (JSON inválido) va directo a la DLQ.
- **Idempotencia del consumidor:** cada evento lleva un `idEvento` (UUID). `notificaciones` guarda los ids procesados y descarta los repetidos, porque RabbitMQ puede entregar un mensaje más de una vez.

Formato del evento:

```json
{
  "idEvento": "6a1f...",
  "tipo": "pedido.estado_cambiado",
  "version": 1,
  "ocurridoEn": "2026-10-09T14:30:00Z",
  "datos": { "idPedido": "...", "idComprador": "...", "estadoAnterior": "en_produccion", "estadoNuevo": "listo" }
}
```

Que el evento se publique sí o sí después de guardar el cambio, aunque el servicio se caiga entre medio, lo resuelve la bandeja de salida (outbox) de [ADR-015](ADR-015-consistencia-y-concurrencia.md).

## Alternativas consideradas

- **Todo síncrono** (`pedidos` llama a `notificaciones` por HTTP). Más simple, pero una caída del correo o de `notificaciones` frenaría los cambios de estado de los pedidos.
- **Kafka.** Pensado para volúmenes y reprocesamiento que no tenemos; operarlo localmente cuesta más.
- **gRPC entre servicios.** Más eficiente, pero suma generación de código y complica depurar; con este volumen HTTP/JSON alcanza.

## Consecuencias

- Los avisos al comprador pueden llegar unos segundos después del cambio de estado.
- Hay que operar RabbitMQ; se suma al `docker-compose` cuando este ADR pase a Aceptado.
- Cada consumidor tiene que ser idempotente y la DLQ necesita que alguien la mire (alerta en D11).
- Los valores de timeout y reintentos son iniciales: se ajustan con los tests de carga ([ADR-013](ADR-013-capacidad-y-costos.md)).

## Validación (Entrega 2)

- **Gateway → servicios:** se mantiene lo decidido (timeout de 3 s, sin reintentos, `503 no_disponible` si el servicio no responde). Validado: el gateway reenvía `/notificaciones*` con `httputil.ReverseProxy`, corta a los 3 s y no reintenta. Con `docker compose stop notificaciones`, un `POST` a `localhost:8080/notificaciones` responde `503 no_disponible` a los 3 s y el gateway sigue atendiendo `/health`.
- **Asíncrono en el flujo propio de notificaciones:** el `POST` responde `202` y el envío del mail pasa en segundo plano, con 3 reintentos tras el primer intento y espera exponencial (30, 60 y 120 s). Confirma la idea de este ADR de no hacer esperar al que pide por una dependencia lenta, aunque todavía sin broker.
- **RabbitMQ** queda para la presentación, cuando exista `pedidos` para publicar `pedido.estado_cambiado`.
- Los mecanismos de protección y la tabla de dependencias de cada servicio están en [ADR-011](ADR-011-resiliencia.md); el circuit breaker hacia inventario en [ADR-010](ADR-010-consumo-de-inventario.md).

## Validación (implementación de pedidos, 26/10/2026)

- **Llamada a `productos`:** timeout de 1 s y 2 reintentos (100 y 200 ms) solo ante timeout, sin conexión, 502, 503 o 504; un 404 es "producto inexistente" y no se reintenta. Probado con un servidor HTTP falso (`backend/pedidos/internal/adaptadores/productos`). Hasta que exista `productos`, ningún pedido se puede crear.
- **Mensajería:** `pedidos` publica en el exchange `pedidos` con confirmación del broker y mensajes persistentes, desde la bandeja de salida (ADR-015). `notificaciones` consume la cola `notificaciones.pedidos` y usa el `idEvento` como `Idempotency-Key`: reutiliza la idempotencia que ya tenía la capacidad publicada, así que un evento repetido no manda otro mail.
- *(propuesto)* **Solo se publica `pedido.estado_cambiado`**, también al crear el pedido (con `estadoAnterior` vacío). `pedido.confirmado` no se publica porque nadie lo consume: es un `estado_cambiado` con `estadoNuevo: confirmado`.
- *(propuesto)* **El evento lleva el email del comprador, el producto, la cantidad y la fecha**, así `notificaciones` arma el mail sin consultar a otro servicio. El email sale de los headers provisorios de identidad (ver [ARCHITECTURE.md](../ARCHITECTURE.md#limitaciones-conocidas-y-deuda-técnica)).
- *(propuesto)* **Reintentos y DLQ con colas quorum:** la cola tiene `x-delivery-limit: 3` y `x-dead-letter-exchange: notificaciones.fallidos`. Una falla transitoria devuelve el mensaje a la cola y RabbitMQ cuenta las entregas; pasado el límite, lo manda a `notificaciones.fallidos`. Un mensaje ilegible, de un tipo o versión desconocidos o con un email inválido va directo a la DLQ. Los reintentos son inmediatos, sin espera entre uno y otro.
- **Medido en Docker:** con RabbitMQ detenido, un pedido nuevo se crea igual (`201`) y sus 2 eventos quedan en la bandeja; al volver RabbitMQ, salen en menos de 25 s y llegan los 2 mails. El consumidor de `notificaciones` se reconecta solo cada 5 s.
- **Traza distribuida:** el `traceparent` viaja en los headers del mensaje. Un `POST /pedidos` queda en Jaeger como una sola traza: api-gateway → pedidos (transacción en MongoDB) → consumidor de notificaciones → MongoDB de notificaciones.
