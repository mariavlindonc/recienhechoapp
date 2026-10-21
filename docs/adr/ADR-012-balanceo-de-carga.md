# ADR-012 (D12): Balanceo de carga

## Estado

Aceptado (Entrega 2; se implementa para la presentación)

## Contexto

El enunciado pide balanceo de carga sobre al menos un servicio con dos o más instancias, que el mecanismo detecte cuándo una instancia no está disponible, y que la distribución y el estado de las instancias se vean en la observabilidad (RNF5).

[ADR-001](ADR-001-limites-de-los-servicios.md) anticipaba a `pedidos` como candidato. Al validar los límites en la Entrega 2 cambiamos a `notificaciones`:

- Es el servicio que está funcionando de punta a punta y el único que recibe tráfico de otro grupo, que no controlamos.
- Ya es apto para varias instancias: no guarda estado en memoria que importe (salvo el límite de uso, ver Consecuencias) y el worker toma cada notificación con un `findOneAndUpdate` atómico, así que dos instancias no envían el mismo mail.
- `pedidos` todavía no existe. Se puede balancear también cuando esté, con el mismo mecanismo.

## Decisión

**Servicio:** `notificaciones`, con **2 instancias** en el `docker-compose` (`notificaciones-1` y `notificaciones-2`), mismo código y misma base MongoDB.

**Quién balancea:** el **api-gateway**, que ya es el proxy de `/notificaciones*`. Recibe la lista de instancias por variable de entorno (`NOTIFICACIONES_URLS=http://notificaciones-1:8084,http://notificaciones-2:8084`).

**Estrategia:** round-robin entre las instancias sanas. Las solicitudes son cortas y parecidas entre sí, así que no hace falta ponderar por carga.

**Verificación de disponibilidad:**

- Activa: el gateway llama a `GET /health` de cada instancia cada 5 s, con timeout de 1 s. Dos fallas seguidas la sacan del balanceo; una respuesta OK la vuelve a sumar.
- Pasiva: si una solicitud a una instancia falla por conexión o timeout, se marca como caída sin esperar al próximo chequeo. El `POST` no se reintenta en otra instancia salvo que traiga `Idempotency-Key` (en nuestro contrato siempre la trae).
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
| **DNS de Docker (`deploy.replicas`)** | Cero código | No detecta instancias caídas ni da métricas por instancia |
| **Balancear `pedidos`** | Es el servicio con más lógica | No va a existir hasta la presentación; no hay forma de demostrarlo en la Entrega 2 |

## Consecuencias

- Cambia lo que anticipaba [ADR-001](ADR-001-limites-de-los-servicios.md) (se actualiza ahí).
- El límite de 60 solicitudes por minuto vive en memoria de cada instancia: con 2 instancias un grupo podría llegar a 120. Se acepta por ahora; si hace falta un límite exacto, el contador pasa a Redis.
- Para la caída controlada alcanza con `docker compose stop notificaciones-1`: el tráfico pasa a la otra instancia y se ve en el tablero.
- Si se suman instancias, solo cambia `NOTIFICACIONES_URLS`.
