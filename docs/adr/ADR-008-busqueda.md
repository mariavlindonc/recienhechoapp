# ADR-008 (D6): Búsqueda

## Estado

Propuesto (Entrega 2; se acepta al implementar `productos`, para la presentación)

## Contexto

El enunciado pide búsqueda sobre una entidad con un motor de búsqueda, con paginación, filtros relevantes, al menos un orden, índice sincronizado con los cambios y un retraso máximo definido.

La entidad es el **producto** (C3). Es el catálogo de una sola pastelería: decenas o cientos de productos, que cambian poco. Lo que importa al comprador:

- que tolere errores de tipeo y tildes ("limon" encuentra "limón", "chocolte" encuentra "chocolate");
- filtrar por categoría, rango de precio y apto celíacos;
- ordenar por precio y por novedad;
- páginas de 20 (criterio de aceptación de C3).

El retraso máximo entre un cambio en `productos` y su aparición en la búsqueda es de **10 segundos** (RNF1).

## Decisión

**Motor: Meilisearch** (`getmeili/meilisearch`), un contenedor propio que solo usa `productos`.

**Índice `productos`**, un documento por producto:

| Campo | Uso |
|---|---|
| `id` | Clave primaria |
| `nombre`, `descripcion`, `categoria` | Búsqueda de texto (en ese orden de relevancia) |
| `categoria`, `precioCentavos`, `aptoCeliacos`, `disponible` | Filtros |
| `precioCentavos`, `creadoEn` | Orden |
| `imagenUrl`, `anticipacionMinimaDias` | Solo se muestran |

- Los productos dados de baja se borran del índice; los no disponibles quedan con `disponible: false` y el frontend filtra por defecto los disponibles.
- Paginación por número de página, 20 por página.
- La búsqueda entra por el api-gateway a `productos` (`GET /productos/busqueda`), que consulta Meilisearch. El frontend no habla con el motor.

**Sincronización (outbox en MySQL):**

1. Cada alta, baja o modificación de un producto escribe, en la misma transacción de MySQL, una fila en la tabla `indice_pendiente` (id de producto y operación).
2. Un proceso dentro de `productos` lee esa tabla cada 2 segundos, manda los cambios a Meilisearch y borra las filas confirmadas.
3. Si Meilisearch no responde, las filas quedan y se reintentan en la vuelta siguiente: no se pierde ningún cambio.

Así el retraso normal es de 2 a 3 segundos, dentro de los 10 de RNF1. Se mide con una métrica de la antigüedad de la fila más vieja en `indice_pendiente`.

**Reconstrucción completa:** un comando `productos reindexar` carga todos los productos de MySQL en un índice nuevo y lo intercambia con el actual (operación de swap de Meilisearch), sin dejar la búsqueda vacía mientras corre. Se usa al cambiar la configuración del índice o si se sospecha que quedó desincronizado.

**Si Meilisearch se cae:** `GET /productos/busqueda` responde `503` y el frontend ofrece el listado del catálogo por categoría, que sale de MySQL con caché ([ADR-009](ADR-009-cache.md)). Se pierde la búsqueda de texto, pero se puede seguir comprando.

## Alternativas consideradas

| Opción | A favor | En contra |
|---|---|---|
| **Meilisearch (elegida)** | Tolerancia a errores de tipeo y tildes, filtros, orden y paginación de fábrica; un contenedor de unos 100 MB; configuración simple | Menos usado que Elasticsearch; sin agregaciones complejas (no las necesitamos) |
| **Elasticsearch u OpenSearch** | Estándar de la industria, muy flexible | JVM y entre 1 y 2 GB de RAM para un catálogo de cientos de productos; más configuración para lograr la tolerancia a tildes |
| **Typesense** | Parecido a Meilisearch | Cualquiera de los dos alcanza; el grupo eligió uno |
| **`LIKE` o `FULLTEXT` de MySQL** | Sin contenedor extra | No tolera errores de tipeo y el enunciado pide un motor de búsqueda |
| **Sincronizar justo después del commit, sin outbox** | Más simple | Si Meilisearch falla en ese momento, el cambio se pierde hasta la próxima reconstrucción |

Si la cátedra espera Elasticsearch, se cambia el adaptador del motor y el contenedor; el outbox y la API de búsqueda quedan igual.

## Consecuencias

- Un contenedor más en el `docker-compose` y una clave maestra de Meilisearch en el `.env`.
- La búsqueda tiene consistencia eventual: hasta 10 segundos de retraso aceptados.
- `productos` tiene que mantener el outbox y el proceso que lo vacía.
- El precio que se ve en la búsqueda puede estar unos segundos desactualizado; el que vale es el que `pedidos` pide a `productos` al crear el pedido ([ADR-002](ADR-002-persistencia.md)).
