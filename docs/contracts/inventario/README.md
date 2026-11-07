# Contrato de Inventario

Le ofrecemos al otro grupo consultar el catálogo de productos y el stock disponible de cada sucursal, y aumentar o reducir ese stock. No se pueden crear productos ni sucursales: se trabaja con los que ya están cargados. El contrato completo está en [openapi.yaml](openapi.yaml) y las decisiones detrás están en [ADR-017](../adr/ADR-017-contrato-propio.md).

**Versión vigente: 2.1.0**, la misma que figura en `info.version` de [openapi.yaml](openapi.yaml).

## Dónde está

Inventario está publicado en `https://stock-volcamelli-inventario.onrender.com`. Los pedidos van directo a esa URL, sin pasar por el resto de nuestro sistema.

Inventario publicado tiene su propia base, separada de la de nuestro sistema. El stock que el grupo consumidor aumenta o reduce es el de esa base: no toca el nuestro, y lo que hacemos nosotros no cambia el suyo. Arranca con los mismos productos, sucursales y stock de prueba, y después cada grupo cambia el suyo.

## Operaciones

| Método y ruta | Para qué |
|---|---|
| `GET /api/products?page=1&size=20` | Listar productos, de a una página |
| `GET /api/products/{productId}` | Ver un producto |
| `GET /api/branches/{branchId}/stock` | Ver el stock disponible de todos los productos de una sucursal. Con `?lowStock=true`, solo los que tienen 5 unidades o menos |
| `GET /api/branches/{branchId}/products/{productId}/stock` | Ver el stock disponible de un producto en una sucursal |
| `POST /api/branches/{branchId}/products/{productId}/stock/increase` | Aumentar el stock de un producto en una sucursal |
| `POST /api/branches/{branchId}/products/{productId}/stock/decrease` | Reducir el stock de un producto en una sucursal |

Algunas aclaraciones:
- Las sucursales se identifican por su `branchId`. Hay tres: 1, 2 y 3. Aumentar o reducir el stock de otra sucursal devuelve `404`. Si sumamos o sacamos sucursales, actualizamos esta lista y le avisamos al grupo consumidor, así no tiene que usar ningún otro servicio nuestro para conocerlas.
- El stock que devolvemos (`quantity`) es el **disponible**: el total menos las unidades reservadas para transferencias a otras sucursales que todavía no llegaron a destino. Por ejemplo, si una sucursal tiene 46 unidades y 4 están reservadas, devolvemos 42.
- Si una sucursal no tiene stock cargado, la consulta de su stock devuelve una lista vacía.
- Si un producto existe pero no tiene stock en esa sucursal, la cantidad viene en 0.
- El stock es el del momento de la consulta y puede cambiar enseguida.
- Cada respuesta trae el header `X-Track-ID`. Si algo sale mal, pasanos ese valor y encontramos el pedido en nuestros logs.

## Aumentar y reducir stock

Los dos pedidos llevan el mismo cuerpo, con la cantidad de unidades y el motivo:

```json
{ "quantity": 5, "reason": "Llegó mercadería" }
```

- `quantity` es un entero mayor a 0, y `reason` no puede estar vacío ni pasar los 200 caracteres.
- La respuesta es el stock disponible del producto en la sucursal después del cambio, con el mismo formato que la consulta de stock.
- Si el producto no tenía stock en esa sucursal, el aumento arranca desde 0.
- La reducción no puede restar más que el disponible: si no alcanza, responde `409` y el stock no cambia.
- Cada cambio se aplica entero o no se aplica. Si llegan dos cambios a la vez sobre el mismo producto y sucursal, se hacen uno después del otro.

**Si no llega la respuesta.** Un pedido repetido se aplica dos veces: si se repite una reducción de 2, se restan 4. Cuando un aumento o una reducción no recibe respuesta (por ejemplo, porque venció el tiempo de espera), hay que consultar el stock de ese producto antes de repetirlo, para ver si el cambio se aplicó.

## Autenticación

Todos los pedidos llevan la API key del grupo consumidor en el header `X-API-Key`. La clave la pasamos por un medio privado y no está en el repositorio. No hay login ni token que renovar: la misma clave sirve hasta que la cambiemos, y si la cambiamos, se la pasamos antes.

## Errores

Todos los errores tienen el mismo formato:

```json
{ "code": "PRODUCT_NOT_FOUND", "message": "No existe el producto 15." }
```

| HTTP | `code` | Qué pasó | Qué hacer |
|---|---|---|---|
| 400 | `INVALID_REQUEST` | Algún parámetro o dato del cuerpo es inválido | Corregir el pedido |
| 401 | `UNAUTHORIZED` | Falta la API key o no es válida | Revisar el header `X-API-Key` |
| 404 | `PRODUCT_NOT_FOUND` | El producto no existe | Verificar el id |
| 404 | `BRANCH_NOT_FOUND` | La sucursal no existe (al aumentar o reducir) | Usar 1, 2 o 3 |
| 409 | `INSUFFICIENT_STOCK` | El disponible no alcanza para la reducción | Consultar el stock y pedir una cantidad menor |
| 500 | `INTERNAL_ERROR` | Error de nuestro lado | Probar de nuevo más tarde |

Si Inventario se está reiniciando o no anda, la respuesta no llega desde Inventario sino desde Render, con un `502` o `503` y una página propia en lugar de este formato. En ese caso hay que probar de nuevo más tarde.

## Ejemplos

Listar productos:

```bash
curl "https://stock-volcamelli-inventario.onrender.com/api/products?page=1&size=20" \
  -H "X-API-Key: <api-key>"
```

Stock de un producto en una sucursal:

```bash
curl https://stock-volcamelli-inventario.onrender.com/api/branches/1/products/1/stock \
  -H "X-API-Key: <api-key>"
```

Respuesta:

```json
{ "branchId": 1, "productId": 1, "productName": "Harina 000", "quantity": 42 }
```

Reducir en 2 el stock de ese producto:

```bash
curl -X POST https://stock-volcamelli-inventario.onrender.com/api/branches/1/products/1/stock/decrease \
  -H "X-API-Key: <api-key>" \
  -H "Content-Type: application/json" \
  -d '{"quantity": 2, "reason": "Venta"}'
```

Respuesta:

```json
{ "branchId": 1, "productId": 1, "productName": "Harina 000", "quantity": 40 }
```

## Cambios en el contrato

Solo hacemos cambios que no rompan lo que ya usa el otro grupo, como agregar operaciones o campos nuevos en las respuestas. No sacamos ni renombramos campos, ni cambiamos su tipo o su significado. Si alguna vez hiciera falta un cambio así, lo acordamos antes con el grupo consumidor. Conviene que el consumidor ignore los campos que no conoce, así un campo nuevo no le rompe nada.

Cada cambio sube la versión de `info.version` y queda anotado en la tabla de abajo:

- si agregamos una operación o un campo, sube el número del medio (por ejemplo, de 2.0.0 a 2.1.0);
- si solo corregimos la documentación, sin cambiar lo que responde la API, sube el último (de 2.0.0 a 2.0.1);
- si tenemos que hacer un cambio que rompa lo que ya usa el otro grupo, primero lo acordamos con ellos y sube el primero (de 2.0.0 a 3.0.0).

| Versión | Qué cambió |
|---|---|
| 1.0.0 | Versión inicial: login, listado y detalle de productos, stock de una sucursal y stock de un producto en una sucursal |
| 2.0.0 | La autenticación pasa a ser una API key en `X-API-Key`. Se saca el login y el token, y la URL pasa a ser la de Inventario publicado. Las cuatro consultas y sus respuestas no cambian ([ADR-017](../adr/ADR-017-contrato-propio.md)) |
| 2.1.0 | Se agregan aumentar y reducir el stock de un producto en una sucursal, sobre la base de Inventario publicado. Las consultas no cambian |
