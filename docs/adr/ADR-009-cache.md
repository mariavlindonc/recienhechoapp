# ADR-009 (D7): Caché

## Estado

Propuesto (Entrega 2; se acepta al implementar `productos`, para la presentación)

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

La búsqueda de texto no se cachea: la resuelve Meilisearch ([ADR-008](ADR-008-busqueda.md)).

**Vigencia e invalidación:**

- TTL de **60 segundos** en todas las claves, como red de seguridad.
- Al modificar o borrar un producto, `productos` borra `producto:{id}` e incrementa `catalogo:version`. Las páginas con la versión vieja dejan de leerse y vencen solas por TTL. Así no hace falta buscar y borrar cada página.
- La invalidación se hace después del commit en MySQL.

**Si Redis falla o tarda:** timeout de 50 ms en cada operación con Redis. Si falla, se lee de MySQL y se registra en logs y métricas; el comprador no lo nota más que en la latencia. Redis no es una dependencia obligatoria de `productos`.

**Precios y pedidos:** `pedidos` nunca usa la caché para el precio que cobra; le pregunta a `productos` por un endpoint interno que lee MySQL directo.

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
