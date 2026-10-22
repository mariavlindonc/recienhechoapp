# ADR-002 (D3): Persistencia

## Estado

- **Aceptado** para `notificaciones` (MongoDB), Entrega 2.
- **Propuesto** para `usuarios`, `productos` y `pedidos`: se acepta al implementar cada servicio.

## Contexto

El enunciado pide al menos un almacenamiento relacional y uno no relacional, elegidos según cómo se accede a los datos, y caché en al menos un flujo de lectura importante. Cada servicio tiene su propia base ([ADR-001](ADR-001-limites-de-los-servicios.md)).

Patrones de acceso esperados:

- **usuarios:** pocas escrituras, lectura por email en cada login, unicidad del email.
- **productos:** se leen mucho más de lo que se modifican; filtros por categoría y precio; imágenes que pesan.
- **pedidos:** cada pedido tiene una cantidad variable de ítems, y los pedidos especiales traen datos libres (diseño, texto, alergias). Se leen enteros (por id, o los de un comprador o un día). La reserva de cupo diario no puede sobrevender.
- **notificaciones:** se escriben una vez, cambian de estado pocas veces y se buscan por id. La clave de idempotencia se consulta antes de crear, y el proceso de envío busca las pendientes por fecha.

## Decisión

| Servicio | Almacenamiento | Por qué |
|---|---|---|
| usuarios | MySQL | Datos tabulares, email con restricción `UNIQUE` |
| productos | MySQL + Redis (caché) | Catálogo estructurado con filtros; Redis guarda el catálogo y el detalle de producto porque cambian poco y se piden mucho |
| pedidos | MongoDB | El pedido es un documento con sus ítems adentro y se lee entero, sin joins; los pedidos especiales tienen campos variables |
| notificaciones | MongoDB | Documentos simples; la clave de idempotencia es única por (grupo, `Idempotency-Key`) para no duplicar envíos |

**Motor relacional: MySQL.** Lo usamos en la materia y el grupo ya lo conoce, así que no suma curva de aprendizaje. Cubre lo que necesitan `usuarios` y `productos`: restricciones `UNIQUE`, índices compuestos para filtrar el catálogo por categoría y precio, y transacciones (InnoDB).

Precios y montos se guardan como enteros en centavos (`int64`), nunca como decimales con coma flotante.

**Cupo de producción.** Cada día tiene un documento en `pedidos` con la capacidad y lo ya reservado. La reserva se hace con un único update condicional atómico (`reservado + cantidad <= capacidad`): si no matchea, no hay cupo. Dentro de un documento MongoDB garantiza atomicidad, así que no hacen falta transacciones multidocumento. El tratamiento completo (concurrencia, reintentos, fallas parciales) va en D4.

**Notificaciones.** Una base MongoDB propia (`mongo:8`). La colección `notificaciones` guarda cada envío con su estado; la consulta es por id y siempre filtrando por el grupo dueño de la clave de API, así que un grupo no ve las notificaciones de otro. Las claves de idempotencia van en la colección `claves_idempotencia`, con `_id` compuesto por (grupo, `Idempotency-Key`): al ser el `_id`, MongoDB rechaza en forma atómica una segunda inserción con el mismo par, sin necesitar otro índice único. Un índice TTL (`vencimiento_24h`, sobre `creadaEn`) las borra a las 24 h, como dice el contrato; así vence la clave pero la notificación se puede seguir consultando. MongoDB corre el borrado más o menos una vez por minuto, así que el vencimiento no es exacto al segundo. Un segundo índice, `pendientes_por_fecha` sobre (`estado`, `proximoIntento`), le permite al proceso de envío encontrar rápido las notificaciones listas.

**Imágenes.** No se guardan en la base: se guarda la URL. Dónde se alojan se decide al implementar `productos`.

## Alternativas consideradas

- **PostgreSQL en lugar de MySQL.** Ofrece más (JSONB, índices parciales), pero para tablas de usuarios y un catálogo no lo necesitamos, y con MySQL el grupo tiene más experiencia.
- **MySQL para todo.** Simplifica la operación y daría transacciones en `pedidos`, pero no cumple el requisito de dos tipos de almacenamiento, y los pedidos especiales con campos libres quedarían en columnas JSON.
- **MongoDB para productos.** El catálogo es tabular y se filtra por varios campos; ahí un esquema relacional con restricciones encaja mejor.
- **Caché en memoria de cada instancia** (en lugar de Redis). Más simple, pero con varias instancias cada una tendría su propia copia y se complica invalidar.

## Consecuencias

- Hay que operar tres motores (MySQL, MongoDB, Redis). Cada uno se suma al `docker-compose` cuando se acepta para su servicio: MongoDB de `notificaciones` entra en la Entrega 2.
- No hay joins entre servicios; las vistas que cruzan datos aceptan consistencia eventual.
- La caché de productos puede mostrar un precio viejo por unos segundos. El precio que vale es el que `pedidos` le pide a `productos` al crear el pedido, sin caché. La vigencia y la invalidación se definen en D7.
- En MongoDB los esquemas no los controla la base: hay que validarlos en el código del servicio.

## Validación (Entrega 2)

MongoDB para `notificaciones` se sostiene con el servicio implementado:

- Los patrones de acceso se confirmaron: se escribe una notificación, se consulta por id y el proceso de envío toma las pendientes, con un agregado que no estaba previsto (el índice de pendientes, ver abajo). La toma es un `findOneAndUpdate` atómico, sin transacciones.
- La base resuelve sola la unicidad de la clave de idempotencia (`_id` compuesto) y su vencimiento a las 24 h (índice TTL). El repositorio tiene tests de integración contra un MongoDB real: se activan con `MONGO_URI_TEST` y se saltean si la variable no está.
- El esquema vive en el código del servicio, como anticipaba este ADR (tipos `documentoNotificacion` y `documentoClave` del adaptador).

Lo que cambió respecto de lo escrito antes:

- La unicidad sale del `_id` de la clave y no necesita un índice aparte. El par que se controla es (grupo, `Idempotency-Key`); antes decíamos (clave de API, `Idempotency-Key`). Dos claves de API del mismo grupo comparten idempotencia y visibilidad.
- Se sumó el índice `pendientes_por_fecha`, que este ADR no preveía.

Cada operación contra MongoDB se corta a los 2 s ([ADR-011](ADR-011-resiliencia.md)); sin ese límite el driver esperaba 30 s. Sigue abierto que el compose todavía no levanta MongoDB, y las decisiones de `usuarios`, `productos` y `pedidos` siguen propuestas hasta implementar cada servicio.
