# ADR-017: Pedidos especiales

## Estado

Propuesto el 28/10/2026, con las opciones abiertas. Falta que el equipo elija en los tres puntos de abajo.

## Contexto

El SPEC pide que el comprador solicite un pedido a medida, que el vendedor lo cotice y que el comprador lo acepte o no (C6, VE5), y que la cotización venza a las 48 h (R8). Cuando el vendedor cotiza, el comprador recibe un mail con el precio.

Lo que ya está definido:

- `pedidos` es dueño de los pedidos especiales ([ADR-001](ADR-001-limites-de-los-servicios.md)) y los guarda en MongoDB, con campos libres como diseño, texto o alergias ([ADR-002](ADR-002-persistencia.md)).
- Los estados son `solicitado` → `cotizado` → `aceptado`, `rechazado` o `vencido`. Un especial aceptado "sigue como `pendiente_confirmacion`" ([ARCHITECTURE](../ARCHITECTURE.md)).
- Un pedido normal reserva el cupo del día al entrar en `pendiente_confirmacion` y pasa a `confirmado` cuando el inventario reserva los insumos de la receta (R1, R3).

Lo que no está definido, y por eso los pedidos especiales siguen en la memoria del navegador:

1. **Inventario al aceptar.** Un especial no tiene producto ni receta, así que no hay insumos que reservar, y el proceso que confirma pedidos no tiene qué mandarle al proveedor.
2. **Cupo.** Cuánto cupo ocupa un especial: el cupo se cuenta en unidades de producto.
3. **Aviso de la cotización.** Hoy `pedidos` publica solo `pedido.estado_cambiado`, y `notificaciones` arma el mail según el estado nuevo.

## Decisión

Pendiente. Se proponen las opciones de abajo. La recomendación, a confirmar por el equipo, es **A1, B1 y C1**: lo más corto que cumple el SPEC sin tocar el contrato con el proveedor.

Rutas propuestas, bajo el mismo grupo que ya usa el gateway:

| Ruta | Quién | Qué hace |
|---|---|---|
| `POST /pedidos/especiales` | comprador | Solicita (`descripcion`, `fecha`, datos libres) con `Idempotency-Key` |
| `GET /pedidos/especiales` | comprador: los suyos; vendedor: todos los `solicitado` y `cotizado` | Lista |
| `PUT /pedidos/especiales/:id/cotizacion` | vendedor | Cotiza (`precioCentavos`) |
| `POST /pedidos/especiales/:id/aceptacion` y `/rechazo` | comprador | Responde; después de 48 h, `409 cotizacion_vencida` |

El vencimiento se calcula al leer o responder (cotizado hace más de 48 h es `vencido`), sin un proceso en segundo plano.

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

- Hasta que se decida, el frontend guarda los especiales en la memoria del navegador: el vendedor no ve lo que pide el comprador desde otra pestaña o dispositivo.
- Con A1 o A2, los especiales no pasan por el disyuntor del inventario ([ADR-011](ADR-011-resiliencia.md)).
- Con C1, `notificaciones` tiene que tolerar el campo nuevo y el estado `cotizado`; hoy cae en el mensaje genérico "Tu pedido cambió de estado", que no dice el precio.
