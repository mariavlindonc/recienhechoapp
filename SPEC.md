# Especificación

Alcance y requisitos de Recién hecho. Versión preliminar (Entrega 1).

## Descripción

Recién hecho es un e-commerce de pastelería a pedido. Los compradores buscan en el catálogo y encargan productos para ahora, para una fecha futura o a medida (pedido especial). La pastelería tiene una capacidad de producción limitada por día: el sistema solo acepta pedidos mientras quede cupo y organiza la producción en un calendario.

La acción principal es **encargar un pedido**. No es un alta más: tiene que respetar la capacidad del día, la anticipación mínima de cada producto y la disponibilidad de insumos, y pasa por estados con consecuencias para el negocio.

## Actores

| Actor | Descripción |
|---|---|
| Comprador | Persona que encarga productos y sigue sus pedidos |
| Vendedor | La pastelería: administra el catálogo, el recetario y la producción |
| Grupo consumidor | Sistema de otro grupo que usa nuestra capacidad de notificaciones |
| Grupo proveedor de inventario | Sistema de otro grupo que nos informa y reserva el stock de insumos |

## Funcionalidades del comprador

| Id | Funcionalidad |
|---|---|
| C1 | Registrarse e iniciar sesión |
| C2 | Consultar el catálogo |
| C3 | Buscar productos con filtros (categoría, precio, apto celíacos, etc.), orden y paginación |
| C4 | Hacer un pedido inmediato (para el primer día con cupo) |
| C5 | Hacer un pedido programado para una fecha y franja elegidas |
| C6 | Solicitar un pedido especial (a medida): se envía la solicitud, el vendedor la cotiza más tarde y el comprador la acepta o no |
| C7 | Ver sus pedidos pendientes y su estado |
| C8 | Cancelar un pedido antes de que entre en producción |
| C9 | Recibir un aviso por email cuando el pedido cambia de estado |

## Funcionalidades del vendedor

| Id | Funcionalidad |
|---|---|
| V1 | Alta, baja y modificación de productos (precio, imágenes, anticipación mínima) |
| V2 | Recetario: ingredientes y cantidades por producto |
| V3 | Definir la capacidad de producción de cada día |
| V4 | Ver el calendario de producción: qué hay que hacer cada día |
| V5 | Cotizar, aceptar o rechazar pedidos especiales |
| V6 | Avanzar el estado de los pedidos (en producción, listo, entregado) |

## Reglas de negocio

| Id | Regla |
|---|---|
| R1 | Un pedido se acepta solo si la capacidad del día alcanza. Dos pedidos simultáneos no pueden quedarse con el mismo cupo, y un pedido no puede reservar dos veces. |
| R2 | Cada producto tiene una anticipación mínima (ej. una torta de casamiento, 7 días). No se aceptan pedidos con menos anticipación. |
| R3 | Un pedido se confirma cuando el inventario del proveedor reserva los insumos de la receta. Si el proveedor no responde, el pedido queda `pendiente_confirmacion` y no se rechaza. |
| R4 | El precio queda fijado al crear el pedido; cambios posteriores del catálogo no lo afectan. |
| R5 | Los montos se manejan en centavos (enteros), nunca con decimales. |
| R6 | Un pedido solo pasa por las transiciones válidas (ver [estados de un pedido](docs/ARCHITECTURE.md#estados-de-un-pedido)). No se puede cancelar en producción ni entregar algo que no está listo. |
| R7 | Cancelar libera el cupo del día y los insumos reservados. |
| R8 | Una cotización de pedido especial vence a las 48 h si el comprador no la acepta. |

## Criterios de aceptación

**C3. Buscar productos**
- Dado un catálogo con productos de varias categorías, cuando el comprador filtra por "tortas" y ordena por precio ascendente, entonces ve solo tortas, de la más barata a la más cara, de a 20 por página.
- Dado que el vendedor cambia el precio de un producto, cuando pasa el retraso máximo tolerado (ver requisitos no funcionales), entonces la búsqueda muestra el precio nuevo.

**C4 y C5. Hacer un pedido**
- Dado un día con capacidad 10 y 9 reservados, cuando dos compradores piden 1 unidad al mismo tiempo, entonces se acepta uno solo y el otro recibe "sin cupo para ese día".
- Dado un producto con anticipación mínima de 3 días, cuando el comprador lo pide para mañana, entonces el pedido se rechaza indicando la primera fecha posible.
- Dado que el comprador reintenta el mismo pedido porque se cortó la conexión, cuando llega el segundo intento, entonces no se crea un pedido duplicado ni se reserva el cupo dos veces.
- Dado que el inventario del proveedor no responde, cuando el comprador hace un pedido, entonces el pedido queda `pendiente_confirmacion` y el comprador lo ve así.

**C6 y V5. Pedido especial**
- Dado un pedido especial solicitado, cuando el vendedor lo cotiza, entonces el comprador recibe un email con el precio y puede aceptarlo dentro de las 48 h.
- Dado un pedido especial cotizado hace más de 48 h, cuando el comprador intenta aceptarlo, entonces se le informa que la cotización venció.

**C8. Cancelar**
- Dado un pedido `confirmado`, cuando el comprador lo cancela, entonces pasa a `cancelado` y el cupo del día vuelve a estar disponible.
- Dado un pedido `en_produccion`, cuando el comprador intenta cancelarlo, entonces se le informa que ya no se puede.

**C9. Avisos**
- Dado un pedido que pasa a `listo`, cuando el cambio se guarda, entonces el comprador recibe un único email aunque el evento se entregue más de una vez.

**V3 y V4. Capacidad y calendario**
- Dado un día con 6 reservados, cuando el vendedor intenta bajar la capacidad a 5, entonces el sistema no lo permite.

## Integraciones externas

| Integración | Rol | Uso |
|---|---|---|
| Notificaciones | **Publicamos** | Otros sistemas nos piden enviar emails a sus usuarios. Contrato y documentación en [docs/contracts](docs/contracts/README.md). |
| Inventario | **Consumimos** | Al confirmar un pedido, `pedidos` consulta y reserva los insumos de la receta. Detalle en D9 cuando el otro grupo publique su contrato. |

## Requisitos no funcionales

Valores preliminares; se ajustan con los tests de carga.

| Id | Requisito |
|---|---|
| RNF1 | Un cambio en un producto aparece en la búsqueda en menos de 10 segundos. |
| RNF2 | Consultar el catálogo responde en menos de 300 ms (p95) con caché. |
| RNF3 | Crear un pedido responde en menos de 2 s (p95), incluida la llamada a inventario. |
| RNF4 | Si inventario o notificaciones fallan, el resto del sistema sigue tomando pedidos (degradación controlada). |
| RNF5 | Al menos un servicio corre con dos o más instancias balanceadas. |
| RNF6 | Logs estructurados, métricas y un tablero permiten seguir un pedido de punta a punta. |
| RNF7 | Todo el sistema se levanta con un solo comando (`docker compose up`), sin configuración manual. |
| RNF8 | Las contraseñas se guardan hasheadas y nunca se devuelven en una respuesta. Los secretos van en variables de entorno, fuera del repositorio. |
