# ADR-003 (D5): Comunicación entre servicios

## Estado

Propuesto (versión inicial; se valida en la Entrega 2)

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

Que el evento se publique sí o sí después de guardar el cambio (sin perderlo si el servicio se cae entre medio, por ejemplo con un outbox) es parte de D4.

## Alternativas consideradas

- **Todo síncrono** (`pedidos` llama a `notificaciones` por HTTP). Más simple, pero una caída del correo o de `notificaciones` frenaría los cambios de estado de los pedidos.
- **Kafka.** Pensado para volúmenes y reprocesamiento que no tenemos; operarlo localmente cuesta más.
- **gRPC entre servicios.** Más eficiente, pero suma generación de código y complica depurar; con este volumen HTTP/JSON alcanza.

## Consecuencias

- Los avisos al comprador pueden llegar unos segundos después del cambio de estado.
- Hay que operar RabbitMQ; se suma al `docker-compose` cuando este ADR pase a Aceptado.
- Cada consumidor tiene que ser idempotente y la DLQ necesita que alguien la mire (alerta en D11).
- Los valores de timeout y reintentos son iniciales: se ajustan con los tests de carga (D13).
