# ADR-008 (D6): Búsqueda

## Estado

Aceptado por Agustín el 26/10/2026. Implementado con `productos` el mismo día; lo marcado con *(propuesto)* se confirma en el PR.

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

- Los productos dados de baja se borran del índice; los no disponibles quedan con `disponible: false` y el frontend filtra por defecto los disponibles (`disponible=true` en la búsqueda).
- `categoria` es el nombre de la categoría. Las categorías son una tabla con ABM del vendedor ([ADR-002](ADR-002-persistencia.md)): renombrar una vuelve a indexar sus productos, por la misma bandeja de salida.
- Paginación por número de página, 20 por página.
- La búsqueda entra por el api-gateway a `productos` (`GET /productos/busqueda`), que consulta Meilisearch. El frontend no habla con el motor.

**Sincronización (outbox en MySQL):**

1. Cada alta, baja o modificación de un producto escribe, en la misma transacción de MySQL, una fila en la tabla `indice_pendiente` (id de producto y operación).
2. Un proceso dentro de `productos` lee esa tabla cada 2 segundos, manda los cambios a Meilisearch y borra las filas cuando Meilisearch completa la tarea de indexación (sus operaciones son asíncronas).
3. Si Meilisearch no responde, las filas quedan y se reintentan en la vuelta siguiente: no se pierde ningún cambio.

Así el retraso normal es de pocos segundos (la vuelta de 2 s más lo que tarde Meilisearch en indexar), dentro de los 10 de RNF1. Se mide con una métrica de la antigüedad de la fila más vieja en `indice_pendiente`.

**Reconstrucción completa:** un comando `productos reindexar` carga todos los productos de MySQL en un índice nuevo y lo intercambia con el actual (operación de swap de Meilisearch), sin dejar la búsqueda vacía mientras corre. Se usa al cambiar la configuración del índice o si se sospecha que quedó desincronizado.

**Si Meilisearch se cae:** `GET /productos/busqueda` responde `503` y el frontend ofrece el listado del catálogo por categoría, que sale de MySQL con caché ([ADR-009](ADR-009-cache.md)). Se pierde la búsqueda de texto, pero se puede seguir comprando.

## Alternativas consideradas

| Opción | A favor | En contra |
|---|---|---|
| **Meilisearch (elegida)** | Tolerancia a errores de tipeo y tildes, filtros, orden y paginación de fábrica; un contenedor de unos 100 MB; configuración simple | Menos usado que Elasticsearch; sin agregaciones complejas (no las necesitamos) |
| **Elasticsearch u OpenSearch** | Estándar de la industria, muy flexible | JVM y entre 1 y 2 GB de RAM para un catálogo de cientos de productos; más configuración para lograr la tolerancia a tildes |
| **Typesense** | Parecido a Meilisearch | Cualquiera de los dos cumple; la diferencia es de preferencia y el motor se puede cambiar sin tocar el diseño |
| **`LIKE` o `FULLTEXT` de MySQL** | Sin contenedor extra | No tolera errores de tipeo y el enunciado pide un motor de búsqueda |
| **Sincronizar justo después del commit, sin outbox** | Más simple | Si Meilisearch falla en ese momento, el cambio se pierde hasta la próxima reconstrucción |

Si la cátedra espera Elasticsearch, se cambia el adaptador del motor y el contenedor; el outbox y la API de búsqueda quedan igual.

## Consecuencias

- Un contenedor más en el `docker-compose` y una clave maestra de Meilisearch en el `.env`.
- La búsqueda tiene consistencia eventual: hasta 10 segundos de retraso aceptados.
- `productos` tiene que mantener el outbox y el proceso que lo vacía.
- El precio que se ve en la búsqueda puede estar unos segundos desactualizado; el que vale es el que `pedidos` pide a `productos` al crear el pedido ([ADR-002](ADR-002-persistencia.md)).

## Validación (implementación de productos, 26/10/2026)

Cliente oficial `github.com/meilisearch/meilisearch-go` v0.36.3; servidor `getmeili/meilisearch:v1.54.3`.

**API:** `GET /productos/busqueda` con estos parámetros:

| Parámetro | Para qué |
|---|---|
| `q` | Texto a buscar |
| `categoria` | Nombre de la categoría |
| `precioMin`, `precioMax` | Rango de precio, en centavos |
| `aptoCeliacos`, `disponible` | `true` o `false` |
| `orden` | `precio_asc`, `precio_desc` o `novedad`; sin él, por relevancia |
| `pagina` | Desde 1 |

Responde `{productos, pagina, porPagina, total}`.

**Medido:**
- Tests de integración contra Meilisearch real (`MEILI_URL_TEST`):
  - "limon" encuentra "limón", y "chocolte" encuentra "chocolate";
  - cumple el criterio de aceptación de C3 (tortas de la más barata a la más cara);
  - un nombre de categoría con comillas no puede cambiar el filtro, porque los valores van escapados;
  - mientras se reconstruye, la búsqueda sigue con el índice vigente.
- Prueba de punta a punta con el compose:
  - un producto nuevo aparece en la búsqueda a los 3 s;
  - con Meilisearch detenido, la búsqueda responde `503 busqueda_no_disponible` y el catálogo sigue respondiendo;
  - un cambio de precio hecho en ese momento aparece solo 2 s después de que Meilisearch vuelve.
- La métrica `indice_pendiente_antiguedad_segundos` mide el retraso. *(propuesto)* La alerta `BusquedaAtrasada` se dispara si pasa los 10 s durante 1 minuto.

**Decisiones de implementación** *(propuesto)*:
- `creadoEn` se guarda como milisegundos desde 1970, para ordenar por novedad.
- Guardar y borrar esperan a que Meilisearch termine la tarea, revisando cada 50 ms y hasta 30 s. Recién ahí se borran las filas de la bandeja. Cada búsqueda se corta a los 2 s.
- Si Meilisearch no responde al arrancar, `productos` arranca igual y reintenta configurar el índice cada 5 s. Mientras tanto, los cambios esperan en la bandeja.
- La bandeja se lee de a 100 filas, y la misma vuelta sigue mientras el lote venga lleno: renombrar una categoría con cientos de productos se refleja en una sola vuelta. Si un producto aparece varias veces, vale su última operación. Si al indexarlo ya no existe, se borra del índice.
- La reconstrucción carga el índice `productos_reconstruccion`, lo intercambia con `productos` y borra el viejo. Si falla, descarta el nuevo y el vigente no cambia. El comando es `docker compose exec productos /servicio reindexar`.
- Con dos instancias de `productos`, las dos pueden procesar las mismas filas. No hace daño, porque guardar y borrar se pueden repetir. Un producto que cambia mientras corre `reindexar` puede quedar desactualizado hasta su próximo cambio.
