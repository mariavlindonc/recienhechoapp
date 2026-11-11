# ADR-017: Pedidos especiales

## Estado

Propuesto el 28/10/2026. Implementado como dice la Decisión, en `develop` (PR #64).

Revisión del 02/11/2026 (revisión del #64): el comprador puede cancelar un especial `solicitado`, y el vendedor puede rechazarlo sin cotizarlo, con un mensaje que le llega al comprador por mail; una cotización vence también si ya pasó la fecha del pedido, y no se cotiza una fecha que ya pasó.

## Contexto

El SPEC pide que el comprador solicite un pedido a medida, que el vendedor lo cotice y que el comprador lo acepte o no (C6, VE5), y que la cotización venza a las 48 h (R8). Cuando el vendedor cotiza, el comprador recibe un mail con el precio.

Lo que ya está definido:

- `pedidos` es dueño de los pedidos especiales ([ADR-001](ADR-001-limites-de-los-servicios.md)) y los guarda en MongoDB, con campos libres como diseño, texto o alergias ([ADR-002](ADR-002-persistencia.md)).
- Los estados son `solicitado` → `cotizado` → `aceptado`, `rechazado` o `vencido`. Un especial aceptado "sigue como `pendiente_confirmacion`" ([ARCHITECTURE](../ARCHITECTURE.md)).
- Un pedido normal reserva el cupo del día y los insumos de la receta al entrar en `pendiente_confirmacion` y pasa a `confirmado` cuando lo confirma el vendedor (R1, R3, [ADR-020](ADR-020-reserva-y-consumo-de-insumos.md)).

Lo que los docs no definen:

1. **Inventario al aceptar.** Un especial no tiene producto ni receta, así que no hay insumos que reservar ni que descontar en el proveedor.
2. **Cupo.** Cuánto cupo ocupa un especial: el cupo se cuenta en unidades de producto.
3. **Aviso de la cotización.** Hoy `pedidos` publica solo `pedido.estado_cambiado`, y `notificaciones` arma el mail según el estado nuevo.

## Decisión

**Implementado:** un especial aceptado "sigue como `pendiente_confirmacion`" al pie de la letra, es decir, por el mismo camino que cualquier pedido:

- Se guarda como un pedido de tipo `especial` con un solo ítem (la descripción, cantidad 1) y sin insumos.
- Al aceptar, reserva 1 unidad del cupo del día en la misma transacción (B1) y pasa a `pendiente_confirmacion`. Sin cupo, `409 sin_cupo` y sigue `cotizado`.
- Lo confirma el vendedor, como cualquier pedido (A2, [ADR-020](ADR-020-reserva-y-consumo-de-insumos.md)). Sin insumos, no reserva nada en el stock propio y al pasar a producción no llama al proveedor.
- Cada cambio publica `pedido.estado_cambiado` con el total (C1). `notificaciones` arma un mail propio para `solicitado`, `cotizado` (con el precio y las 48 h), `rechazado` y `vencido`, y uno con el mensaje del vendedor cuando él rechaza la solicitud.
- Los especiales sin aceptar no aparecen en el calendario del día (`GET /pedidos?fecha=`).


Rutas:

| Ruta | Quién | Qué hace |
|---|---|---|
| `POST /pedidos/especiales` | comprador | Solicita (`descripcion`, `fecha`) con `Idempotency-Key` |
| `GET /pedidos/especiales` | vendedor | Lista los `solicitado` y `cotizado`; el comprador ve los suyos en `GET /pedidos` |
| `PUT /pedidos/especiales/:id/cotizacion` | vendedor | Cotiza (`precioCentavos`); si la fecha del pedido ya pasó, `409 transicion_invalida` |
| `POST /pedidos/especiales/:id/declinacion` | vendedor | Rechaza un `solicitado` sin cotizarlo, con `mensaje` (de 1 a 500 caracteres) para el comprador. Pasa a `cancelado` con `mensajeVendedor`, sin cupo ni insumos que devolver |
| `POST /pedidos/especiales/:id/aceptacion` y `/rechazo` | comprador | Responde; después de 48 h o de la fecha del pedido, `409 cotizacion_vencida` |
| `POST /pedidos/:id/cancelacion` | comprador | Retira un especial `solicitado`: pasa a `cancelado` sin cupo ni insumos que devolver. Un `cotizado` no se cancela, se rechaza |

El vencimiento se controla cuando el comprador responde, como pide el SPEC: pasadas 48 h desde la cotización, o si la fecha del pedido ya pasó, el especial queda `vencido` y la respuesta es `409 cotizacion_vencida`, con la causa en el mensaje. No hay un proceso en segundo plano, así que hasta ese momento sigue figurando como `cotizado`.

## Alternativas consideradas

**1. Inventario al aceptar**

| Opción | Cómo funciona | A favor | En contra |
|---|---|---|---|
| **A1. Sin inventario** | Al aceptar, el especial queda `confirmado`; el vendedor ya contempló los insumos al cotizar | No cambia el contrato con el proveedor ni el proceso de confirmación | Se aparta de "sigue como `pendiente_confirmacion`" |
| **A2. Queda pendiente y el vendedor confirma** | Queda `pendiente_confirmacion` y el vendedor lo pasa a `confirmado` a mano | Fiel al texto de ARCHITECTURE | Un paso más para el vendedor; la regla R3 ("Un pedido se confirma cuando el inventario del proveedor reserva los insumos de la receta") deja de valer para especiales |
| **A3. El vendedor carga insumos al cotizar** | La cotización lleva insumos y el pedido sigue el camino normal con el proveedor | Mismo flujo que un pedido normal | Más trabajo en la pantalla del vendedor y en la API; depende del contrato del proveedor, que todavía no existe (D9) |

**2. Cupo**

| Opción | A favor | En contra |
|---|---|---|
| **B1. Ocupa 1 unidad del día al aceptar** | Simple; usa la misma reserva atómica que los pedidos normales (ADR-015) | Una torta de casamiento cuenta igual que un budín |
| **B2. El vendedor fija las unidades al cotizar** | Refleja el trabajo real | Un dato más al cotizar |

**3. Aviso de la cotización**

| Opción | A favor | En contra |
|---|---|---|
| **C1. Reusar `pedido.estado_cambiado`** con estado `cotizado` y el precio en los datos; `notificaciones` suma un caso al armar el mail | Sin evento ni cola nuevos | El evento lleva un campo más, opcional (versión 1 compatible) |
| **C2. Evento nuevo `pedido.cotizado`** | Separa los especiales de los pedidos normales | Otra routing key, otra versión que soportar en el consumidor |

## Consecuencias

- El evento suma los campos opcionales `totalCentavos` y `mensajeVendedor`; la versión sigue en 1 porque un consumidor viejo los ignora ([ADR-003](ADR-003-comunicacion-entre-servicios.md)).
- Los especiales no pasan por el proveedor de inventario ni por su disyuntor ([ADR-011](ADR-011-resiliencia.md)): no tienen insumos que descontar, y el proveedor exige una cantidad mayor a 0.
