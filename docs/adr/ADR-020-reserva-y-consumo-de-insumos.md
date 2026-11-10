# ADR-020: Confirmación del vendedor, reserva propia y consumo de insumos

## Estado

Aceptado el 06/11/2026 ([#84](https://github.com/mariavlindonc/recienhechoapp/issues/84)). Reemplaza a [ADR-010](ADR-010-consumo-de-inventario.md). Los `productId` del proveedor salen de productos suyos usados como casilleros (ver [De dónde salen los `productId`](#de-dónde-salen-los-productid)). Que marcar un pedido para el vendedor devuelva su reserva lo decidió Agustín el 10/11/2026.

## Contexto

[ADR-010](ADR-010-consumo-de-inventario.md) confirmaba cada pedido automáticamente: reservar era reducir el stock en el proveedor, y liberar era volver a aumentarlo. Eso mezclaba dos momentos distintos:

- **Reserva:** los insumos quedan comprometidos para un pedido, pero siguen en la alacena.
- **Consumo:** los insumos salen de verdad cuando el pedido entra en producción.

Mezclarlos hacía que cancelar aumentara un stock que nunca se había movido, y que un pedido que nunca reservó nada igual intentara liberar (encontrado en la revisión del #59).

La versión 2.1.0 del contrato del proveedor ([copia](../contracts/inventario/README.md)) define lo que se puede hacer:

- Ofrece `increase` y `decrease` por producto y sucursal, de a un producto por llamada. No tiene reservas.
- **No son idempotentes:** un pedido repetido se aplica dos veces. Si no llega la respuesta, hay que consultar el stock antes de repetir.
- `decrease` responde `409 INSUFFICIENT_STOCK` si no alcanza, y el stock no cambia.
- Las dos operaciones devuelven el stock que queda.
- Trabajan sobre una base que usa **solo nuestro grupo**, pero solo con productos ya cargados: no se pueden crear, y un id desconocido da `404`.

Además, en una pastelería chica el vendedor quiere revisar cada pedido antes de comprometerse: una regla automática no resuelve mejor que esa persona si faltan insumos.

## Decisión

### Flujo del pedido

```
Comprador encarga
   └─► pendiente_confirmacion   reserva el cupo y los insumos, aunque no alcancen
Vendedor revisa
   ├─ confirma ─► confirmado
   └─ rechaza  ─► cancelado     devuelve el cupo y la reserva
Vendedor pasa a producción
   └─► en_produccion            consume los insumos en el proveedor
```

**Todos los pedidos los confirma el vendedor**, también los especiales aceptados. Ya no se confirma ni se cancela nada automáticamente por el inventario. El vendedor ve los pedidos pendientes de confirmar, con una **nota en los que tienen insumos faltantes** (por ejemplo, "Faltan 200 g de harina"). Puede confirmarlos igual: decide él.

### Stock y reservas en su propia base

El stock vive en una base propia, `inventario`, separada de la base `pedidos`, en el mismo servidor MongoDB (`mongo-pedidos`). Guarda, por ingrediente y unidad, el stock conocido, lo reservado y el `productId` del proveedor. El servicio dueño sigue siendo `pedidos`, que es el único que la usa ([ADR-002](ADR-002-persistencia.md)).

Estar en el mismo servidor importa: MongoDB permite una transacción entre dos bases del mismo replica set, así que el pedido, el cupo y la reserva se siguen guardando juntos ([ADR-015](ADR-015-consistencia-y-concurrencia.md)).

- **Al encargar** se reservan los insumos de la receta, en la misma transacción que el cupo (R1). La reserva nunca rechaza un pedido: si no alcanza, queda negativo el disponible y aparece la nota.
- **Disponible = stock conocido − reservado.**
- **Al cancelar o rechazar** se devuelve la reserva en la misma transacción, sin llamar al proveedor.
- **Stock conocido:** se actualiza con la cantidad que devuelven `increase` y `decrease`.

### Consumo al pasar a `en_produccion`

Pasar a `en_produccion` no espera al proveedor: se guarda el estado y el pedido queda con el consumo pendiente. Un proceso en segundo plano llama a `decrease` por cada insumo, **de a un pedido por vez**, y guarda el avance por insumo. Así, si falla a la mitad, no repite lo que ya se descontó.

| Respuesta de `decrease` | Qué hace `pedidos` |
|---|---|
| OK | Guarda el stock devuelto, baja lo reservado y sigue con el próximo insumo |
| `409 INSUFFICIENT_STOCK` | Marca el pedido para que lo revise el vendedor. No frena la producción |
| Timeout (1,5 s), 5xx o sin conexión | Se reintenta con esperas que se duplican (1, 2, 4, 8 minutos) hasta un máximo de 10 |
| 4xx inesperado (error nuestro o cambio de contrato) | No se reintenta. Alerta para el equipo, y el pedido queda marcado para el vendedor |

**Cómo no descontar dos veces.** Si `decrease` no respondió, no se sabe si se aplicó. Antes de repetir se consulta `GET /api/branches/{branchId}/products/{productId}/stock`, **solo en este caso**, y se compara con el stock conocido:

- Si bajó exactamente esa cantidad, se aplicó: se da por hecho y no se repite.
- Si no cambió, no se aplicó: se repite.
- Si dio otra cosa, no se puede saber: se marca para el vendedor.

**Marcar un pedido para el vendedor devuelve lo que le quedaba reservado**, en la misma transacción. Ese consumo ya no se reintenta solo, así que nada más va a usar esa reserva; si quedara, el disponible bajaría para siempre y las notas de faltantes saldrían de más.

La comparación es confiable porque esa base la usa solo nuestro grupo, `pedidos` corre con una sola instancia, y el consumo va de a un pedido por vez. Lo mismo vale para `increase`.

Los pedidos sin insumos, como los especiales ([ADR-017](ADR-017-pedidos-especiales.md)), no llaman al proveedor, porque `quantity` tiene que ser mayor a 0.

### Carga de stock

El vendedor tiene una pantalla simple: elige un ingrediente y una cantidad, y eso llama a `increase` y guarda el stock que devuelve. Es la forma de cargar el stock inicial, que arranca en 0, y de reponerlo.

### Traducción al proveedor

**Puerto propio como capa anticorrupción.** El dominio no conoce el formato, los nombres ni los errores del proveedor: los traduce el adaptador. `pedidos` define un puerto con reducir, aumentar y consultar el stock de un producto.

- `quantity` va en **nuestras unidades**: 500 g son `quantity: 500`. Se puede porque esa base la usamos solo nosotros.
- `reason` lleva el motivo, por ejemplo el id del pedido.
- **Una sola sucursal:** la pastelería no tiene sucursales, pero el contrato pide `branchId`, así que va fijo por configuración.
- La URL y la clave de API van en variables de entorno. La clave va en `.env`, nunca en el repositorio.
- Sigue el adaptador falso configurable (`INVENTARIO_MODO`) para los tests, la demo y las caídas provocadas.

**Circuit breaker** sobre el adaptador ([ADR-011](ADR-011-resiliencia.md)): con 5 fallas seguidas se abre por 30 s, y el consumo espera sin llamar al proveedor. Un `409` o un 4xx no cuentan como falla.

**Test de contrato:** levanta Prism con la copia del OpenAPI del proveedor y corre el adaptador real contra él. Si el proveedor cambia el contrato de forma incompatible, actualizar la copia hace fallar el test.

**Mock del proveedor como respaldo:** el servicio del proveedor es de otro grupo y corre en Render, así que no controlamos si va a estar publicado, despierto y andando el día de la demo (ver [Pendientes](#pendientes), punto 1). Cada pieza ajena en el camino suma una chance de falla. Por eso el compose tiene `inventario-mock`: el mismo Prism del test de contrato, en el puerto 4011. Si el proveedor no responde, se apunta su URL al mock y el adaptador real, el timeout, los errores y el disyuntor se muestran igual. El mock no guarda estado y responde siempre los ejemplos del contrato; con `Prefer: code=409` fuerza un `409`.

**Observabilidad:** métricas de llamadas al proveedor por resultado y latencia, estado del circuit breaker y pedidos con consumo pendiente. Una alerta cuando el proveedor rechaza una operación por un error de formato.

### Qué ve cada uno

| Situación | Comprador | Vendedor |
|---|---|---|
| Pedido recién encargado | "Recibimos tu pedido" (`pendiente_confirmacion`), por pantalla y por mail | Lo ve entre los pendientes, con la nota si faltan insumos |
| El vendedor confirma | `confirmado`, por pantalla y por mail | |
| El vendedor rechaza | `cancelado`, con el motivo | |
| El proveedor falla al consumir | No ve nada: el pedido sigue en producción | Si no se resuelve solo, lo ve marcado |

### De dónde salen los `productId`

El proveedor exige un producto que ya exista en su base: no deja crearlos y un id desconocido da `404`. Cada ingrediente nuestro guarda el `productId` de un producto suyo ya cargado, que usamos como casillero, ignorando su nombre. Mientras un ingrediente no tiene `productId`, no se llama al proveedor y el pedido queda marcado para el vendedor.

Los casilleros se reparten solos, en el orden de `INVENTARIO_PRODUCTOS_IDS`, la primera vez que un ingrediente los necesita (al cargar su stock o al consumirlo): el primer producto libre es para el primer ingrediente. Un producto nunca sirve a dos ingredientes.

Por qué casilleros:
- **Anda hoy.** Depende solo de lo que el proveedor ya tiene cargado, y sirve igual contra su servicio real y contra el mock.
- **No depende de que el otro grupo haga algo.** Pedirles que carguen nuestros ingredientes exige que respondan, y su servicio no respondía el 06/11 ni el 08/11, con la entrega cerrando el miércoles 11/11 a las 23:59.
- **El nombre no hace falta.** `quantity` va en nuestras unidades y esa base la usa solo nuestro grupo ([Traducción al proveedor](#traducción-al-proveedor)), así que lo único que importa del producto es que exista y que su stock sea nuestro.
- **El diseño no cambia si después se carga lo nuestro.** Es el mismo `productId` por ingrediente: pasar a ingredientes cargados es cambiar esos valores.

Qué cuesta:
- El nombre que devuelve el proveedor (por ejemplo "Harina 000") no tiene por qué coincidir con el ingrediente. Los datos de esa base no se pueden leer como stock real de la pastelería.
- Hay tantos ingredientes distintos como productos tenga cargados el proveedor: si no alcanzan, los que sobran quedan sin `productId` y marcados para el vendedor.

## Pendientes

1. **El servicio del proveedor anda, en otra dirección que la del contrato.** El 06/11 y el 08/11/2026, la URL del contrato (`stock-volcamelli-inventario.onrender.com`) devolvía `404` con `x-render-routing: no-server`: Render no tiene ningún servicio con ese nombre. El 09/11 el grupo del proveedor confirmó que anda. La dirección que responde es `https://stock-volcamelli.onrender.com`, sin `-inventario`. La URL se corrigió en el contrato, en su README, en el adaptador y en [DEMO.md](../DEMO.md).

   Probado el 09/11 contra el servicio real, con la API key que nos pasó el grupo (va en `X-API-Key` y no se escribe en el repositorio):

   | Prueba | Resultado |
   |---|---|
   | `GET /health` | `200`, `{"status":"ok"}` |
   | Cualquier ruta de `/api` sin clave | `401 UNAUTHORIZED`, con el mismo mensaje del contrato |
   | `GET /api/products?page=1&size=5` | `200`, 10 productos en total |
   | `GET /api/products/9999` | `404 PRODUCT_NOT_FOUND` |
   | `GET /api/branches/1/products/1/stock` | `200`, `quantity: 0` |
   | `POST .../stock/decrease` de 1 unidad con stock 0 | `409 INSUFFICIENT_STOCK`; el stock sigue en 0 |

   Todas respondieron en 0,4 a 0,7 s con el servicio despierto, bien por debajo del timeout de 1,5 s. Queda sin probar: `increase`, que escribe en su base; el tiempo cuando el servicio tarda en despertar; y que la sucursal 4 responde `200` con lista vacía al consultar, aunque el contrato solo promete `BRANCH_NOT_FOUND` al aumentar o reducir.

2. **El stock del proveedor está vacío.** La sucursal 1 no tiene stock cargado, así que cualquier reducción da `409` hasta que se haga un `increase` sobre los productos que usamos como casilleros.

## Alternativas consideradas

**Dónde vive el stock:**

- **En la base `pedidos`.** Lo más simple, pero mezcla el stock con los pedidos.
- **En un MongoDB propio, en otro contenedor.** Separación total, pero no hay transacción entre dos servidores: si se guarda el pedido y falla la reserva, quedan desparejos, y crear un pedido depende de una base más.
- **En un servicio nuevo de inventario.** Los límites más prolijos, pero suma un servicio entero, y reservar pasa a ser una llamada que puede fallar.

**Dónde viven las reservas:**

- **En el proveedor.** Una sola fuente de verdad, pero el contrato no ofrece reservas.
- **Sin reservas.** Lo más simple, pero dos pedidos pueden contar con los mismos insumos, y el vendedor confirmaría sin ver lo que falta.

**Confirmación:**

- **Automática con el inventario, como en ADR-010.** No necesita al vendedor, pero exige reconfirmaciones, y cancela solo pedidos que la pastelería podría haber salvado.

**Cómo no descontar dos veces:**

- **Mandar cada timeout a revisión del vendedor.** Nunca descuenta dos veces, pero suma trabajo manual cada vez que el proveedor tarda.
- **Repetir igual.** Puede descontar dos veces.

**Compras pendientes:**

- **Una lista de lo que hay que comprar, que se resuelve al registrar la compra.** Queda como futura escala: por ahora alcanza con la nota en cada pedido y la carga de stock.

**Stock:**

- **Consultar el stock del proveedor en cada pedido.** Sería siempre el dato real, pero crear un pedido volvería a depender del proveedor.

## Consecuencias

- Crear un pedido ya no depende del proveedor: no llama a nadie, y RNF3 y RNF4 se cumplen sin degradación. El proveedor solo afecta al consumo, que puede atrasarse sin frenar la producción.
- `pendiente_confirmacion` pasa a significar "espera al vendedor". Un pedido puede quedar ahí hasta que el vendedor lo revise, y el comprador lo ve así.
- El vendedor suma trabajo: confirmar cada pedido y cargar el stock. Hasta que lo cargue, todos los pedidos van a tener la nota de insumos faltantes.
- Desaparecen la reconfirmación en segundo plano, la marca a las 24 h, la cancelación por falta de insumos y la liberación en el proveedor. Liberar insumos ya no llama a nadie, así que se resuelven los dos problemas de la revisión del #59 ([#84](https://github.com/mariavlindonc/recienhechoapp/issues/84)).
- La base `inventario` comparte servidor con la base `pedidos`: se cae con ella, y si algún día se separan, se pierde la transacción.
- Nuestro stock puede diferir del de la base del proveedor si alguien la cambia por afuera: la comparación después de un timeout lo detecta y lo manda al vendedor.
- Si `pedidos` pasara a tener varias instancias, habría que asegurar que el consumo siga yendo de a un pedido por vez.
- Cambian R3 y R7 del SPEC, los criterios de aceptación de C4 y C5, [ADR-002](ADR-002-persistencia.md), [ADR-011](ADR-011-resiliencia.md), [ADR-015](ADR-015-consistencia-y-concurrencia.md), [ADR-017](ADR-017-pedidos-especiales.md) y la tabla de infraestructura de AGENTS.md. Se actualizan al implementar.
