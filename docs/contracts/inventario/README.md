# Contrato de Inventario

Le ofrecemos al otro grupo consultar nuestro catálogo de productos y el stock disponible de cada sucursal. Son solo lecturas: no se puede crear ni modificar nada. El contrato completo está en [openapi.yaml](openapi.yaml) y las decisiones detrás están en [ADR-017](../adr/ADR-017-contrato-propio.md).

**Versión vigente: 2.0.0**, la misma que figura en `info.version` de [openapi.yaml](openapi.yaml).

## Dónde está

Inventario está publicado en `https://stock-volcamelli-inventario.onrender.com`. Las consultas van directo a esa URL, sin pasar por el resto de nuestro sistema.

## Operaciones

| Método y ruta | Para qué |
|---|---|
| `GET /api/products?page=1&size=20` | Listar productos, de a una página |
| `GET /api/products/{productId}` | Ver un producto |
| `GET /api/branches/{branchId}/stock` | Ver el stock disponible de todos los productos de una sucursal. Con `?lowStock=true`, solo los que tienen 5 unidades o menos |
| `GET /api/branches/{branchId}/products/{productId}/stock` | Ver el stock disponible de un producto en una sucursal |

Algunas aclaraciones:
- Las sucursales se identifican por su `branchId`. Con los datos de prueba hay tres: 1, 2 y 3. Si sumamos o sacamos sucursales, actualizamos esta lista y le avisamos al grupo consumidor, así no tiene que usar ningún otro servicio nuestro para conocerlas.
- El stock que devolvemos (`quantity`) es el **disponible**: el total menos las unidades reservadas para transferencias a otras sucursales que todavía no llegaron a destino. Por ejemplo, si una sucursal tiene 46 unidades y 4 están reservadas, devolvemos 42.
- Si una sucursal no tiene stock cargado, la consulta de su stock devuelve una lista vacía.
- Si un producto existe pero no tiene stock en esa sucursal, la cantidad viene en 0.
- El stock es el del momento de la consulta y puede cambiar enseguida.
- Cada respuesta trae el header `X-Track-ID`. Si algo sale mal, pasanos ese valor y encontramos el pedido en nuestros logs.

## Autenticación

Todas las consultas llevan la API key del grupo consumidor en el header `X-API-Key`. La clave la pasamos por un medio privado y no está en el repositorio. No hay login ni token que renovar: la misma clave sirve hasta que la cambiemos, y si la cambiamos, se la pasamos antes.

## Errores

Todos los errores tienen el mismo formato:

```json
{ "code": "PRODUCT_NOT_FOUND", "message": "No existe el producto 15." }
```

| HTTP | `code` | Qué pasó | Qué hacer |
|---|---|---|---|
| 400 | `INVALID_REQUEST` | Algún parámetro es inválido | Corregir el pedido |
| 401 | `UNAUTHORIZED` | Falta la API key o no es válida | Revisar el header `X-API-Key` |
| 404 | `PRODUCT_NOT_FOUND` | El producto no existe | Verificar el id |
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
