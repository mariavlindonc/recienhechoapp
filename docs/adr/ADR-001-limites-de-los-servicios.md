# ADR-001 (D1): Límites de los servicios

## Estado

Aceptado

## Contexto

Recién hecho es un e-commerce de pastelería a pedido. Hay que repartir el sistema en al menos tres microservicios, más un api gateway, y decidir qué datos maneja cada uno. Además tenemos que publicar una capacidad para otro grupo (notificaciones) y consumir otra (inventario).

La regla de negocio más delicada es la capacidad de producción: la pastelería puede hacer una cantidad limitada de productos por día, y dos compradores no pueden quedarse con el mismo cupo.

## Decisión

Separamos por capacidad de negocio. Cada servicio es el único dueño de sus datos y tiene su propia base: ningún otro servicio la lee ni la escribe. Si un servicio necesita un dato de otro, lo pide por API o lo recibe por un evento.

| Servicio | Responsabilidad | Datos propios |
|---|---|---|
| usuarios | Registro, login y roles (comprador o vendedor) | Cuentas, roles, contraseñas hasheadas |
| productos | Catálogo, búsqueda y recetario | Productos, precios en centavos, imágenes, recetas |
| pedidos | Pedidos inmediatos, programados y especiales; calendario y capacidad de producción | Pedidos y sus estados, cupos por día |
| notificaciones | Envío de avisos a compradores y a usuarios de otros grupos | Notificaciones, estados de envío, claves de idempotencia |
| api-gateway | Punto de entrada, autenticación y ruteo | Ninguno |

Criterios:

- **La capacidad de producción va en `pedidos`.** Reservar el cupo del día y crear el pedido tienen que pasar juntos o no pasar: si estuvieran en servicios distintos haría falta una transacción distribuida. Por eso el calendario vive donde se crean los pedidos.
- **El pedido guarda una copia del producto (nombre y precio).** Cambiar un precio no puede alterar pedidos ya hechos, y así mostrar un pedido no depende de `productos`.
- **Notificaciones es un servicio propio.** Lo usan `pedidos` y otros grupos, tiene otro ritmo (envío en segundo plano, reintentos contra un servidor de correo) y otro contrato público. Si estuviera adentro de `pedidos`, una caída del correo o un pico de pedidos externos afectaría la venta.
- **El consumo de inventario lo hace `pedidos`**, porque es quien confirma pedidos y necesita reservar insumos. No pasa por el gateway ni por el frontend.

Relaciones:

- Frontend → api-gateway → usuarios, productos, pedidos.
- pedidos → productos (síncrono: precio y receta al crear el pedido).
- pedidos → inventario del otro grupo (síncrono, al confirmar).
- pedidos → notificaciones (asíncrono, por eventos de cambio de estado).
- Otros grupos → api-gateway → notificaciones.

## Alternativas consideradas

- **Calendario de producción como servicio aparte.** Separa mejor el tema "producción", pero obliga a coordinar dos servicios en la operación que no puede duplicar ni perder cupos. Se descarta por ahora; se puede separar más adelante si crece.
- **Notificaciones dentro de pedidos.** Menos piezas, pero mezcla un contrato público con la lógica interna y acopla sus fallas.
- **Base de datos compartida.** Más fácil de consultar, pero cualquier cambio de esquema rompe a los demás servicios y se pierde la idea de dueño único.

## Consecuencias

- No hay consultas que crucen bases: las vistas que juntan datos de varios servicios se arman con llamadas o con copias, y aceptan consistencia eventual.
- `pedidos` concentra la lógica más compleja.
- El servicio con balanceo de carga es `notificaciones` ([ADR-012](ADR-012-balanceo-de-carga.md)).

## Validación (Entrega 2)

Con `notificaciones` implementado, los límites se sostienen:

- `notificaciones` se implementó sin leer ni escribir datos de otro servicio: solo su MongoDB, su servidor de correo y el contrato publicado. El `POST` no espera al servidor de correo, así que una falla del correo no se propaga a quien lo llama, que era el motivo para separarlo. Que su caída tampoco afecte las demás rutas del gateway se confirma cuando el gateway lo enrute (todavía solo expone `/health`).
- La arquitectura interna de cada servicio ([ADR-007](ADR-007-arquitectura-interna.md)) encaja con estos límites sin mover responsabilidades.
- Escribir búsqueda, caché e inventario ([ADR-008](ADR-008-busqueda.md), [ADR-009](ADR-009-cache.md), [ADR-010](ADR-010-consumo-de-inventario.md)) no obligó a compartir datos: el índice y la caché son de `productos`, y el consumo de inventario queda en `pedidos`.
- Lo que cambia: este ADR anticipaba a `pedidos` como candidato al balanceo de carga. Se balancea `notificaciones`, porque es el servicio que ya está implementado y el que va a recibir tráfico externo; los motivos están en [ADR-012](ADR-012-balanceo-de-carga.md).

Queda por validar con `pedidos` funcionando que el calendario de producción no necesite separarse.
