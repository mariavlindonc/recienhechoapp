# ADR-012 (D12): Balanceo de carga

## Estado

Aceptado (Entrega 2; se implementa para la presentación)

Implementación: todavía no hay balanceo. El api-gateway solo expone `/health` y el compose tiene una sola instancia de `notificaciones`. A `notificaciones` le falta el endpoint `/vivo` que el gateway va a consultar.

## Contexto

El enunciado pide balanceo de carga sobre al menos un servicio con dos o más instancias, que el mecanismo detecte cuándo una instancia no está disponible, y que la distribución y el estado de las instancias se vean en la observabilidad (RNF5 y RNF6).

[ADR-001](ADR-001-limites-de-los-servicios.md) anticipaba a `pedidos` como candidato. Al validar los límites en la Entrega 2 cambiamos a `notificaciones`:

- Es el servicio que ya está implementado y probado con tests, y el único que va a recibir tráfico de otro grupo, que no controlamos.
- Ya es apto para varias instancias: no guarda estado en memoria que importe (salvo el límite de uso, ver Consecuencias) y el proceso de envío toma cada notificación con un `findOneAndUpdate` atómico que la aparta por 2 minutos, así que dos instancias no envían el mismo mail. La excepción es que venza el apartado antes de guardar el resultado: el mail puede salir dos veces ([limitaciones conocidas](../ARCHITECTURE.md#limitaciones-conocidas-y-deuda-técnica)).
- `pedidos` todavía es un esqueleto que solo expone `/health`. Se puede balancear también cuando esté implementado, con el mismo mecanismo.

## Decisión

**Servicio:** `notificaciones`, con **2 instancias** en el `docker-compose` (`notificaciones-1` y `notificaciones-2`), mismo código y misma base MongoDB. Las instancias no publican puerto al host: se entra solo por el api-gateway.

**Quién balancea:** el **api-gateway**, que va a ser el proxy de `/notificaciones*`. Recibe la lista de instancias por variable de entorno (`NOTIFICACIONES_URLS=http://notificaciones-1:8084,http://notificaciones-2:8084`).

**Estrategia:** round-robin entre las instancias sanas. Las solicitudes son cortas y parecidas entre sí, así que no hace falta ponderar por carga.

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

## Consecuencias

- Cambia lo que anticipaba [ADR-001](ADR-001-limites-de-los-servicios.md) (se actualiza ahí).
- El límite de 60 solicitudes por minuto vive en memoria de cada instancia: con 2 instancias un grupo podría llegar a 120. Se acepta por ahora; si hace falta un límite exacto, el control de tráfico pasa al api-gateway, que ve todas las solicitudes antes de repartirlas (como ya dice [ARCHITECTURE](../ARCHITECTURE.md#limitaciones-conocidas-y-deuda-técnica)).
- Para la caída controlada alcanza con `docker compose stop notificaciones-1`: el tráfico pasa a la otra instancia y se ve en el tablero.
- Si se suman instancias, solo cambia `NOTIFICACIONES_URLS`.
