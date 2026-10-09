# ADR-002 (D3): Persistencia

## Estado

Propuesto (versión inicial; se valida en la Entrega 2)

## Contexto

El enunciado pide al menos un almacenamiento relacional y uno no relacional, elegidos según cómo se accede a los datos, y caché en al menos un flujo de lectura importante. Cada servicio tiene su propia base ([ADR-001](ADR-001-limites-de-los-servicios.md)).

Patrones de acceso esperados:

- **usuarios:** pocas escrituras, lectura por email en cada login, unicidad del email.
- **productos:** se leen mucho más de lo que se modifican; filtros por categoría y precio; imágenes que pesan.
- **pedidos:** cada pedido tiene una cantidad variable de ítems, y los pedidos especiales traen datos libres (diseño, texto, alergias). Se leen enteros (por id, o los de un comprador o un día). La reserva de cupo diario no puede sobrevender.
- **notificaciones:** se escriben una vez, cambian de estado pocas veces y se buscan por id o por clave de idempotencia.

## Decisión

| Servicio | Almacenamiento | Por qué |
|---|---|---|
| usuarios | PostgreSQL | Datos tabulares, email con restricción `UNIQUE` |
| productos | PostgreSQL + Redis (caché) | Catálogo estructurado con filtros; Redis guarda el catálogo y el detalle de producto porque cambian poco y se piden mucho |
| pedidos | MongoDB | El pedido es un documento con sus ítems adentro y se lee entero, sin joins; los pedidos especiales tienen campos variables |
| notificaciones | MongoDB | Documentos simples; índice único sobre (clave de API, `Idempotency-Key`) para no duplicar envíos |

Precios y montos se guardan como enteros en centavos (`int64`), nunca como decimales con coma flotante.

**Cupo de producción.** Cada día tiene un documento en `pedidos` con la capacidad y lo ya reservado. La reserva se hace con un único update condicional atómico (`reservado + cantidad <= capacidad`): si no matchea, no hay cupo. Dentro de un documento MongoDB garantiza atomicidad, así que no hacen falta transacciones multidocumento. El tratamiento completo (concurrencia, reintentos, fallas parciales) va en D4.

**Imágenes.** No se guardan en la base: se guarda la URL. Dónde se alojan se decide al implementar `productos`.

## Alternativas consideradas

- **PostgreSQL para todo.** Simplifica la operación y daría transacciones en `pedidos`, pero no cumple el requisito de dos tipos de almacenamiento, y los pedidos especiales con campos libres quedarían en columnas JSON.
- **MongoDB para productos.** El catálogo es tabular y se filtra por varios campos; ahí un esquema relacional con restricciones encaja mejor.
- **Caché en memoria de cada instancia** (en lugar de Redis). Más simple, pero con varias instancias cada una tendría su propia copia y se complica invalidar.

## Consecuencias

- Hay que operar tres motores (PostgreSQL, MongoDB, Redis); por ahora solo están propuestos y se suman al `docker-compose` cuando este ADR pase a Aceptado.
- No hay joins entre servicios; las vistas que cruzan datos aceptan consistencia eventual.
- La caché de productos puede mostrar un precio viejo por unos segundos. El precio que vale es el que `pedidos` le pide a `productos` al crear el pedido, sin caché. La vigencia y la invalidación se definen en D7.
- En MongoDB los esquemas no los controla la base: hay que validarlos en el código del servicio.
