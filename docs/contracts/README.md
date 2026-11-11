# Contratos

Un contrato por carpeta: el formal (OpenAPI) y su documentación de uso.

| Capacidad | Rol | Carpeta | Versión | Decisión |
|---|---|---|---|---|
| Notificaciones | **Publicamos**: otros grupos nos piden enviar emails a sus usuarios | [notificaciones/](notificaciones/README.md) | 1.0.1 | [ADR-004](../adr/ADR-004-contrato-de-notificaciones.md) |
| Inventario (Stock Volcamelli) | **Consumimos**: lo publica el grupo proveedor; acá guardamos una copia de su contrato | [inventario/](inventario/README.md) | 2.1.0 | [ADR-020](../adr/ADR-020-reserva-y-consumo-de-insumos.md) |

La versión 2.1.0 de inventario suma aumentar y reducir stock, que es lo que consumimos ([ADR-020](../adr/ADR-020-reserva-y-consumo-de-insumos.md)). Esas operaciones no son idempotentes: un pedido repetido se aplica dos veces. El contrato de inventario es del otro grupo: si publican una versión nueva, se reemplaza la copia y se revisa el test de contrato de `pedidos` (#77). La copia queda tal como la publicaron, salvo la URL de `servers`, que se corrigió a la que responde ([ADR-020](../adr/ADR-020-reserva-y-consumo-de-insumos.md)), así que sus links internos apuntan a documentos de su repositorio: por ejemplo, su `ADR-017-contrato-propio` no es nuestro ADR-017.

## APIs HTTP internas

No tienen contrato formal porque no salen del sistema. La que usa un servicio de otro es `GET /internos/productos/{id}`, que `pedidos` le pide a `productos` para el precio y la receta. El api-gateway no la enruta ([ADR-003](../adr/ADR-003-comunicacion-entre-servicios.md), [ADR-009](../adr/ADR-009-cache.md)).

## Eventos de mensajería

`pedidos` publica `pedido.estado_cambiado` en RabbitMQ, con la bandeja de salida de [ADR-015](../adr/ADR-015-consistencia-y-concurrencia.md), y `notificaciones` lo consume para avisarle al comprador. Formato y reglas en [ADR-003](../adr/ADR-003-comunicacion-entre-servicios.md).
