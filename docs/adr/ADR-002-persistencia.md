# ADR-002 (D3): Persistencia

## Estado

- **Aceptado** para `notificaciones` (MongoDB), Entrega 2.
- **Aceptado** para `pedidos` (MongoDB, replica set por las transacciones de [ADR-015](ADR-015-consistencia-y-concurrencia.md)), implementado el 26/10/2026.
- **Aceptado** para `usuarios` y `productos` (MySQL y Redis) por Agustín el 26/10/2026. `productos` está implementado; `usuarios` no.
- **Aceptado** para `productos` por Agustín el 26/10/2026: acceso a MySQL con GORM, imágenes subidas a `productos`, baja lógica de productos y categorías en una tabla con ABM del vendedor. Lo marcado con *(propuesto)* se confirma en el PR.

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

**Cupo de producción.** Cada día tiene un documento en `pedidos` con la capacidad y lo ya reservado. La reserva se hace con un único update condicional atómico (`reservado + cantidad <= capacidad`): si no matchea, no hay cupo. Dentro de un documento MongoDB garantiza atomicidad. Crear el pedido junto con la reserva y su evento toca varios documentos: eso lo resuelve [ADR-015](ADR-015-consistencia-y-concurrencia.md) (D4) con transacciones.

**Notificaciones.** Una base MongoDB propia (`mongo:8`). La colección `notificaciones` guarda cada envío con su estado; la consulta es por id y siempre filtrando por el grupo dueño de la clave de API, así que un grupo no ve las notificaciones de otro. Las claves de idempotencia van en la colección `claves_idempotencia`, con `_id` compuesto por (grupo, `Idempotency-Key`): al ser el `_id`, MongoDB rechaza en forma atómica una segunda inserción con el mismo par, sin necesitar otro índice único. Un índice TTL (`vencimiento_24h`, sobre `creadaEn`) las borra a las 24 h, como dice el contrato; así vence la clave pero la notificación se puede seguir consultando. MongoDB corre el borrado más o menos una vez por minuto, así que el vencimiento no es exacto al segundo. Un segundo índice, `pendientes_por_fecha` sobre (`estado`, `proximoIntento`), le permite al proceso de envío encontrar rápido las notificaciones listas.

**Acceso a MySQL: GORM** (`gorm.io/gorm` y `gorm.io/driver/mysql`). El grupo lo usó en la materia anterior, el mismo argumento que eligió MySQL. Los structs con tags de GORM viven solo en el adaptador `mysql`: el dominio no sabe que existe ([ADR-007](ADR-007-arquitectura-interna.md)). Las tablas las crea `AutoMigrate` al arrancar. El producto, su receta y la fila de la bandeja del índice ([ADR-008](ADR-008-busqueda.md)) se guardan en una sola transacción (`db.Transaction`).

**Imágenes.** El vendedor sube la foto a `productos` (`POST /productos/{id}/imagen`, multipart). Se acepta JPG, PNG o WebP de hasta 2 MB *(propuesto)*, y el tipo se decide mirando el contenido del archivo, no su nombre. El archivo va al volumen `productos-imagenes` con un nombre al azar, y la base guarda solo la URL (`/imagenes/{archivo}`), que el api-gateway sirve. *(propuesto)* Cambiar la foto no borra la anterior: la búsqueda tarda unos segundos en tener la URL nueva y la caché hasta 60 s, y mientras tanto mostrarían una imagen rota. Los archivos viejos quedan en el volumen, igual que las fotos de los productos dados de baja; si crece, se limpian los que ningún producto usa.

**Productos con baja lógica.** Dar de baja un producto llena `eliminado_en` (`gorm.DeletedAt`): GORM lo saca de todas las consultas, pero la fila queda y se puede recuperar. Los pedidos ya hechos no dependen de esa fila, porque guardan su propia copia del nombre, el precio y los insumos ([ADR-001](ADR-001-limites-de-los-servicios.md)).

**Categorías en una tabla** con su ABM para el vendedor. El nombre es `UNIQUE` y, por la collation de MySQL 8 (`utf8mb4_0900_ai_ci`), "Tortas", "tortas" y "Tórtas" cuentan como el mismo. *(propuesto)* Las categorías se borran de verdad, y solo si no tienen productos activos (si no, `409 categoria_en_uso`): con baja lógica, el nombre de una categoría borrada seguiría ocupando el índice único. Los productos dados de baja que la usaban quedan con `categoria_id` en `NULL`.

## Alternativas consideradas

- **PostgreSQL en lugar de MySQL.** Ofrece más (JSONB, índices parciales), pero para tablas de usuarios y un catálogo no lo necesitamos, y con MySQL el grupo tiene más experiencia.
- **MySQL para todo.** Simplifica la operación y daría transacciones en `pedidos`, pero no cumple el requisito de dos tipos de almacenamiento, y los pedidos especiales con campos libres quedarían en columnas JSON.
- **MongoDB para productos.** El catálogo es tabular y se filtra por varios campos; ahí un esquema relacional con restricciones encaja mejor.
- **`database/sql` con el driver, o sqlx, en lugar de GORM.** El SQL queda a la vista y hay una dependencia menos, pero cada fila se mapea a mano. Con arquitectura hexagonal parte del ahorro de GORM se pierde igual, porque el adaptador convierte sus structs al dominio.
- **Imágenes como link externo** (el vendedor pega la URL de una foto subida en otro sitio). No suma nada al sistema, pero si borran la foto del otro sitio, el producto queda sin imagen. **MinIO** (almacenamiento tipo S3) es lo más realista, pero es otro contenedor que configurar.
- **Borrado real de productos.** Más simple, pero un producto borrado por error no se recupera.
- **Categorías como lista fija en el código**, o como texto libre en el producto. La lista fija obliga a cambiar el código para sumar una; con texto libre, "torta" y "tortas" terminan siendo dos categorías y el filtro de C3 deja productos afuera.
- **Caché en memoria de cada instancia** (en lugar de Redis). Más simple, pero con varias instancias cada una tendría su propia copia y se complica invalidar.

## Consecuencias

- Hay que operar tres motores (MySQL, MongoDB, Redis). Cada uno se suma al `docker-compose` cuando se acepta para su servicio: MongoDB de `notificaciones` entra en la Entrega 2.
- No hay joins entre servicios; las vistas que cruzan datos aceptan consistencia eventual.
- La caché de productos puede mostrar un precio viejo por unos segundos. El precio que vale es el que `pedidos` le pide a `productos` al crear el pedido, sin caché. La vigencia y la invalidación se definen en D7.
- En MongoDB los esquemas no los controla la base: hay que validarlos en el código del servicio.
- El volumen de imágenes es de una sola máquina. Con `productos` repartido en varias máquinas hace falta un almacenamiento compartido.

## Validación (Entrega 2)

MongoDB para `notificaciones` se sostiene con el servicio implementado:

- Los patrones de acceso se confirmaron: se escribe una notificación, se consulta por id y el proceso de envío toma las pendientes, con un agregado que no estaba previsto (el índice de pendientes, ver abajo). La toma es un `findOneAndUpdate` atómico, sin transacciones.
- La base resuelve sola la unicidad de la clave de idempotencia (`_id` compuesto) y su vencimiento a las 24 h (índice TTL). El repositorio tiene tests de integración contra un MongoDB real: se activan con `MONGO_URI_TEST` y se saltean si la variable no está.
- El esquema vive en el código del servicio, como anticipaba este ADR (tipos `documentoNotificacion` y `documentoClave` del adaptador).

Lo que cambió respecto de lo escrito antes:

- La unicidad sale del `_id` de la clave y no necesita un índice aparte. El par que se controla es (grupo, `Idempotency-Key`); antes decíamos (clave de API, `Idempotency-Key`). Dos claves de API del mismo grupo comparten idempotencia y visibilidad.
- Se sumó el índice `pendientes_por_fecha`, que este ADR no preveía.

Cada operación contra MongoDB se corta a los 2 s ([ADR-011](ADR-011-resiliencia.md)); sin ese límite el driver esperaba 30 s. MongoDB ya corre en el compose (`mongo-notificaciones`), y las decisiones de `usuarios`, `productos` y `pedidos` siguen propuestas hasta implementar cada servicio.

## Validación (implementación de productos, 26/10/2026)

MySQL 8.4 con GORM para `productos`, en el compose como `mysql-productos`:

- El adaptador cumple tres puertos (`Categorias`, `Productos` y `BandejaDelIndice`) con una sola base. Tiene tests de integración contra MySQL real: se activan con `MYSQL_DSN_TEST` y se saltean si no está.
- Medido en los tests:
  - si el producto no se guarda (por ejemplo, con una categoría inexistente), tampoco queda su fila en la bandeja;
  - el nombre de la categoría es único sin importar mayúsculas ni tildes;
  - la baja lógica saca el producto de las consultas pero deja la fila.
- *(propuesto)* La fila de la categoría se bloquea (`SELECT ... FOR UPDATE`) antes de contar sus productos y borrarla. Así, un producto que se crea en ella al mismo tiempo espera y después falla por la clave foránea, en vez de quedar activo sin categoría.
- *(propuesto)* La conexión activa `ClientFoundRows`. Sin eso, un `UPDATE` que no cambia ningún valor devuelve "0 filas" y el producto parecía no existir. Lo encontró el test de integración de la receta.
- El índice compuesto es (`categoria_id`, `nombre`), porque el catálogo filtra por categoría y ordena por nombre. Los filtros por precio los resuelve Meilisearch ([ADR-008](ADR-008-busqueda.md)).
- Cada operación con MySQL se corta a los 2 s (`timeout`, `readTimeout` y `writeTimeout` en el DSN, [ADR-011](ADR-011-resiliencia.md)).
