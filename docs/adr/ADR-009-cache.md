# ADR-009 (D7): Caché

## Estado

Aceptado por Agustín el 26/10/2026. Implementado con `productos` el mismo día; lo marcado con *(propuesto)* lo decidió un agente de código y se confirma en el PR.

## Contexto

El enunciado pide caché en al menos un flujo de lectura relevante, justificada por una necesidad observable y con métricas que muestren su impacto.

En Recién hecho el flujo que más se lee es el **catálogo**: la portada, el listado por categoría y el detalle de cada producto. Lo piden todos los compradores en cada visita, y el vendedor lo cambia pocas veces por día. RNF2 pide que consultar el catálogo responda en menos de 300 ms (p95). [ADR-002](ADR-002-persistencia.md) ya eligió Redis para esto.

## Decisión

**Redis** (`redis:7`) como caché de `productos`, con el patrón **cache-aside**: el servicio busca primero en Redis; si no está, lee MySQL, guarda el resultado y lo devuelve.

**Qué se guarda:**

| Clave | Contenido |
|---|---|
| `producto:{id}` | Detalle de un producto |
| `catalogo:v{version}:{categoria}:{pagina}` | Una página del listado del catálogo |
| `catalogo:version` | Número de versión del catálogo |
| `categorias:v{version}` | La lista de categorías para los filtros (se invalida con la misma versión) |

La búsqueda de texto no se cachea: la resuelve Meilisearch ([ADR-008](ADR-008-busqueda.md)).

**Vigencia e invalidación:**

- TTL de **60 segundos** en todas las claves menos `catalogo:version`, que no vence: si se reiniciara el contador, podrían reaparecer páginas viejas. El TTL es la red de seguridad.
- Al modificar o borrar un producto, `productos` borra `producto:{id}` e incrementa `catalogo:version`. Las páginas con la versión vieja dejan de leerse y vencen solas por TTL. Así no hace falta buscar y borrar cada página.
- La invalidación se hace después del commit en MySQL.

**Si Redis falla o tarda:** timeout de 50 ms en cada operación con Redis. Si falla, se lee de MySQL y se registra en logs y métricas; el comprador solo nota la latencia. Redis no es una dependencia obligatoria de `productos`.

**Precios y pedidos:** `pedidos` nunca usa la caché para el precio que cobra; le pregunta a `productos` por la ruta interna `GET /internos/productos/{id}`, que lee MySQL directo e incluye la receta. El api-gateway no la enruta. Su contrato está en [ADR-003](ADR-003-comunicacion-entre-servicios.md).

**Medición:**

- Métricas en `/metrics` de `productos`: `cache_aciertos_total` y `cache_fallos_total` por tipo de clave, y el histograma de latencia de `GET /productos` y `GET /productos/{id}`.
- Se corre el mismo test de carga ([ADR-013](ADR-013-capacidad-y-costos.md)) con la caché apagada (variable `CACHE_HABILITADA=false`) y prendida, y se comparan p95 y tasa de aciertos en el tablero de Grafana.

## Alternativas consideradas

- **Caché en memoria de cada instancia.** Sin contenedor extra, pero con varias instancias cada una tiene su copia y la invalidación no llega a todas.
- **Caché HTTP en el api-gateway o en el navegador.** Rápida, pero invalidar al cambiar un producto es difícil y el vendedor vería sus cambios tarde.
- **Write-through** (escribir en Redis al escribir en MySQL). Mantiene la caché siempre caliente, pero mete a Redis en el camino de escritura y lo vuelve obligatorio.
- **Solo TTL, sin invalidación.** Más simple, pero un cambio de precio tardaría hasta un minuto en verse.

## Consecuencias

- Un contenedor más en el `docker-compose`.
- Entre el commit y la invalidación hay una ventana mínima en la que se puede leer un dato viejo; el TTL la acota a 60 s en el peor caso.
- La comparación antes y después queda como evidencia para la presentación.

## Validación (implementación de productos, 26/10/2026)

Cliente `github.com/redis/go-redis/v9` v9.23.0; servidor `redis:7.4.11`.

- El puerto `CacheDelCatalogo` dice qué se quiere (el producto, una página, invalidar el catálogo) y el adaptador de Redis decide las claves, la vigencia y el formato (JSON). La aplicación no sabe que existe la versión del catálogo ([ADR-007](ADR-007-arquitectura-interna.md)).
- Se cachean el detalle público (sin la receta), las páginas del catálogo del comprador y la lista de categorías. El listado del vendedor, que incluye los productos no disponibles, va siempre a MySQL.
- *(propuesto)* Sin reintentos (`MaxRetries: -1`): un reintento haría esperar más que los 50 ms. Medido en un test, con Redis apagado el error llega enseguida. En la prueba de punta a punta, el detalle respondió en 70 ms leyendo de MySQL.
- Métricas en `/metrics`:
  - `cache_aciertos_total` y `cache_fallos_total` por tipo (`producto`, `catalogo`, `categorias`);
  - `cache_errores_total` por operación, cuando Redis falla.
- Con `CACHE_HABILITADA=false` se usa una caché apagada que nunca encuentra nada. La medición formal antes y después va con los tests de carga ([ADR-013](ADR-013-capacidad-y-costos.md)).
- Ventanas conocidas, acotadas por la vigencia de 60 s:
  - si se invalida entre que se lee MySQL y se guarda una página, puede guardarse una página vieja con la versión nueva;
  - renombrar una categoría no invalida el detalle de cada producto, que muestra el nombre viejo hasta que vence.
