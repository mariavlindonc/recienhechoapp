# ADR-012 (D12): Balanceo de carga

## Estado

Aceptado (Entrega 2; se implementa para la presentación)

Implementación: todavía no hay balanceo. El api-gateway solo expone `/health` y el compose tiene una sola instancia de `notificaciones`. A `notificaciones` le falta el endpoint `/vivo` que el gateway va a consultar.

## Contexto

El enunciado pide balanceo de carga sobre al menos un servicio con dos o más instancias, que el mecanismo detecte cuándo una instancia no está disponible, y que la distribución y el estado de las instancias se vean en la observabilidad (RNF5 y RNF6).

[ADR-001](ADR-001-limites-de-los-servicios.md) anticipaba a `pedidos` como candidato. Al validar los límites en la Entrega 2 cambiamos a `notificaciones`:

- Es el servicio que ya está implementado y probado con tests, y el único que va a recibir tráfico de otro grupo, que no controlamos.
- Ya es apto para varias instancias: no guarda estado en memoria que importe (el límite de uso está en el gateway, ver Consecuencias) y el proceso de envío toma cada notificación con un `findOneAndUpdate` atómico que la aparta por 2 minutos, así que dos instancias no envían el mismo mail. La excepción es que venza el apartado antes de guardar el resultado: el mail puede salir dos veces ([limitaciones conocidas](../ARCHITECTURE.md#limitaciones-conocidas-y-deuda-técnica)).
- `pedidos` todavía es un esqueleto que solo expone `/health`. Se puede balancear también cuando esté implementado, con el mismo mecanismo.

## Decisión

**Servicio:** `notificaciones`, con **2 instancias** en el `docker-compose` (`notificaciones-1` y `notificaciones-2`), mismo código y misma base MongoDB. Las instancias no publican puerto al host: se entra solo por el api-gateway.

**Quién balancea:** el **api-gateway**, que va a ser el proxy de `/notificaciones*`. Recibe la lista de instancias por variable de entorno (`NOTIFICACIONES_URLS=http://notificaciones-1:8084,http://notificaciones-2:8084`).

**Estrategia:** round-robin entre las instancias sanas. Decisión del grupo (María, Athina y Agustín, 22/10/2026), después de comparar las estrategias de reparto (ver "Estrategias de reparto consideradas"): round-robin es simple y alcanza para el sistema actual y su alcance. Hay dos instancias iguales, las solicitudes son cortas y parecidas (p95 de ~5 ms en notificaciones) y no hay estado entre solicitudes. Además, reparte parejo y predecible, que es la evidencia más clara de distribución en el tablero. **Se considera pasar a "menos conexiones"** si los tests de carga ([ADR-013](ADR-013-capacidad-y-costos.md)) muestran que una instancia se pone lenta bajo carga: es la situación en la que round-robin le sigue mandando la mitad del tráfico y "menos conexiones" se adapta sola.

**Verificación de disponibilidad:**

- Activa: el gateway llama a `GET /vivo` de cada instancia cada 5 s, con timeout de 1 s. Dos fallas seguidas la sacan del balanceo; una respuesta OK la vuelve a sumar.
- `/vivo` es un chequeo liviano: responde `200` si el proceso atiende, sin consultar MongoDB. `/health`, que sí hace ping a MongoDB, queda para personas y para el compose. Las dos instancias comparten el mismo MongoDB: si el gateway usara `/health`, una falla de MongoDB sacaría a las dos a la vez, y con MongoDB lento podrían entrar y salir del balanceo sin parar. El balanceo solo tiene que sacar la instancia que se cayó; la caída de MongoDB ya está tratada en [ADR-011](ADR-011-resiliencia.md).
- Pasiva: si una solicitud a una instancia falla por conexión o timeout, se marca como caída sin esperar al próximo chequeo. El gateway no reintenta ([ADR-003](ADR-003-comunicacion-entre-servicios.md)): esa solicitud responde `503` y el consumidor reintenta con la misma `Idempotency-Key`, como indica el contrato, y el reintento ya va a la otra instancia.
- Sin instancias sanas: `503 no_disponible`.

**Observabilidad:**

- Métrica en el gateway `gateway_solicitudes_total{servicio, instancia, estado}`: muestra cómo se reparte el tráfico.
- Métrica `gateway_instancia_sana{servicio, instancia}` (1 o 0): muestra qué instancias están en el balanceo.
- Cada log del gateway incluye la instancia elegida, y la traza muestra a qué instancia fue cada solicitud.
- Un panel de Grafana con las dos métricas.

## Alternativas consideradas

| Opción | A favor | En contra |
|---|---|---|
| **Balanceo en el api-gateway (elegida)** | No suma contenedores; las métricas por instancia salen del mismo lugar que el resto del ruteo; se explica con código propio | Hay que escribir el round-robin y los health checks (unas decenas de líneas) |
| **Nginx o Traefik delante de las instancias** | Probado, con health checks configurables | Un salto y un componente más; en Nginx libre los health checks activos son limitados |
| **Chequear `/health` (con ping a MongoDB) en lugar de `/vivo`** | No suma un endpoint | Ante una falla de MongoDB saca a todas las instancias juntas, y el ping de hasta 2 s obliga a un timeout más largo |
| **DNS de Docker (`deploy.replicas`)** | Cero código | No detecta instancias caídas ni da métricas por instancia |
| **Balancear `pedidos`** | Es el servicio con más lógica | No va a tener lógica hasta la presentación; no hay forma de demostrarlo en la Entrega 2 |

**Estrategias de reparto consideradas** (dónde balancear está en la tabla de arriba; esta compara cómo repartir):

| Estrategia | Cómo reparte | A favor, en nuestro caso | En contra, en nuestro caso |
|---|---|---|---|
| **Round-robin (elegida)** | Por turno: 1, 2, 1, 2... | La más simple; reparto parejo y predecible, ideal para mostrar la distribución en el tablero | Si una instancia está viva pero lenta, sigue recibiendo la mitad del tráfico |
| **Menos conexiones** (*least connections*) | A la instancia con menos solicitudes en curso | Se adapta sola si una instancia se pone lenta: le llega menos tráfico | Con solicitudes de ~5 ms casi siempre las dos tienen 0 en curso y se comporta como round-robin; suma unas líneas de código. **Queda como evolución** si los tests de carga lo muestran necesario |
| **Aleatoria** | Al azar | Simple | Reparto menos parejo con poco tráfico; peor evidencia de distribución |
| **Dos opciones al azar** (*power of two choices*) | Elige dos al azar y manda a la menos cargada | Muy buena con muchas instancias | Con dos instancias equivale a "menos conexiones" |
| **Ponderada** | Más tráfico a la instancia más potente | Útil si las máquinas son distintas | Las instancias son iguales: no aporta |
| **Afinidad por grupo o IP** (*hash*) | Cada grupo siempre a la misma instancia | Haría exacto el límite de 60 solicitudes por minuto | Con un solo grupo consumidor, todo el tráfico externo iría a una sola instancia: no habría balanceo |
| **Por latencia** | A la instancia que viene respondiendo más rápido | La que mejor se adapta | Compleja de implementar y de explicar; no la justifica el volumen esperado |

Lo que más protege no es la estrategia sino la verificación de disponibilidad: con cualquiera de ellas, sin los chequeos de `/vivo`, una instancia caída se lleva su parte del tráfico.

## Consecuencias

- Cambia lo que anticipaba [ADR-001](ADR-001-limites-de-los-servicios.md) (se actualiza ahí).
- El límite de 60 solicitudes por minuto vivía en memoria de cada instancia: con 2 instancias un grupo podía llegar a 120. Pasó al api-gateway, que ve todas las solicitudes antes de repartirlas, así que ahora es exacto. Si algún día hay más de una instancia del gateway, vuelve el mismo problema.
- Para la caída controlada alcanza con `docker compose stop notificaciones-1`: el tráfico pasa a la otra instancia y se ve en el tablero.
- Si se suman instancias, solo cambia `NOTIFICACIONES_URLS`.
