# ADR-018: Seguimiento del pedido en tiempo real con WebSocket

## Estado

Aceptado el 02/11/2026 (#75). Revisado el 03/11/2026: como `pedidos` corre con una sola instancia, los cambios llegan al WebSocket desde la aplicación al guardar, en vez de una cola de RabbitMQ. Revisado el 06/11/2026: el token ya no viaja en la URL; el WebSocket se autentica con la cookie de sesión ([ADR-019](ADR-019-sesion-en-cookie.md)).

## Contexto

El desafío B2 del SPEC pide que el comprador vea los cambios de estado de su pedido sin recargar la página, que reciba solo los de **sus** pedidos y que, si se corta la conexión, la página se reconecte sola y muestre el estado actual.

La idea del equipo va más allá del aviso: es **seguir el pedido en vivo**, y algún día mostrar en un mapa dónde está el delivery, como las apps de envíos. Por eso el WebSocket es parte del pedido, no un canal más de avisos.

Lo que ya existe y condiciona el diseño:

- `pedidos` publica `pedido.estado_cambiado` en el exchange `pedidos` de RabbitMQ desde la bandeja de salida (ADR-015), solo después de guardar el cambio.
- El api-gateway corta cada solicitud a los 3 s (`context.WithTimeout` en `proxy.go`, ADR-003). Una conexión WebSocket dura minutos.
- La API de WebSocket del navegador no permite agregar headers, pero sí manda las cookies del sitio al abrir la conexión ([ADR-019](ADR-019-sesion-en-cookie.md)).
- El servidor del frontend (`frontend/servidor.ts`) reenvía `/api` con `fetch`, que no sirve para WebSocket.
- `pedidos` corre con una sola instancia: los cambios de los pedidos y los compradores conectados están en el mismo proceso.

## Decisión

**El WebSocket vive en `pedidos`**, que es el dueño de los pedidos y de sus eventos. Para la arquitectura hexagonal (ADR-007) es un adaptador de entrada más, `internal/adaptadores/tiemporeal`, al lado de la API HTTP.

**Librería: `github.com/coder/websocket` v1.8.15** (publicada el 15/06/2026). Trabaja con `context`, como el resto del servicio, así los timeouts y el apagado ordenado se manejan igual que en HTTP.

**Ruta:** `GET /pedidos/tiempo-real`, con `Upgrade: websocket`. Solo para compradores: el vendedor recibe `403`.

**Autenticación: la cookie de sesión** ([ADR-019](ADR-019-sesion-en-cookie.md)). El navegador la manda sola al abrir el WebSocket. El api-gateway la verifica como en cualquier solicitud, arma `X-Id-Usuario`, `X-Email-Usuario` y `X-Rol-Usuario`, y la saca antes de reenviar. `pedidos` no ve el token. La ruta queda afuera de las trazas, porque una conexión que dura minutos no dice nada como traza.

**Cómo llegan los cambios al WebSocket: al guardar.** Cuando la aplicación guarda un cambio que trae evento (el mismo `pedido.estado_cambiado` que va a la bandeja de salida), y recién cuando el guardado salió bien, se lo pasa a un puerto nuevo, `AvisosEnVivo`. El adaptador WebSocket lo cumple y se lo manda a los compradores conectados cuyo `X-Id-Usuario` coincide con el `idComprador` del evento. Así lo que llega por el WebSocket ya está en la base, llega al instante y no depende de RabbitMQ. Los mails siguen saliendo por la bandeja de salida, como siempre.

**Formato de los mensajes**, del servidor al navegador, en JSON, con un `tipo` para que se puedan sumar otros sin cambiar el protocolo:

```json
{ "tipo": "estado_cambiado", "idPedido": "…", "estadoAnterior": "confirmado", "estadoNuevo": "en_produccion", "ocurridoEn": "2026-11-02T18:00:00Z" }
```

Más adelante, el mapa del delivery sería otro tipo (por ejemplo, `{ "tipo": "ubicacion", "idPedido": "…", "latitud": …, "longitud": … }`) por la misma conexión. El navegador ignora los tipos que no conoce.

**Reconexión y estado actual.** El WebSocket solo avisa cambios: no es la fuente de verdad. Al conectarse, y cada vez que se reconecta, la pantalla vuelve a pedir sus pedidos por la API HTTP de siempre, así lo que pasó mientras estaba desconectada no se pierde. Se reconecta sola con esperas crecientes (1, 2, 4… hasta 30 s). El servidor manda un ping cada 30 s para detectar conexiones muertas.

**Paso por el api-gateway.** `httputil.ReverseProxy` ya reenvía conexiones con `Upgrade`. La ruta `/pedidos/tiempo-real` queda afuera del corte de 3 s.

**Paso por el servidor del frontend.** `servidor.ts` acepta el WebSocket del navegador en `/api/pedidos/tiempo-real`, rechaza un `Origin` de otro sitio y abre otro WebSocket hacia el gateway con la cookie, copiando los mensajes hacia el navegador. Así el navegador sigue hablando con un solo origen (ADR-001). En desarrollo, el proxy de Vite lo hace con `ws: true`.

**Observabilidad (ADR-006):** la métrica `pedidos_conexiones_tiempo_real` (conexiones abiertas) y `pedidos_mensajes_tiempo_real_total` (mensajes enviados), y logs al conectar y desconectar, con el id del comprador y sin el token.

## Alternativas consideradas

**Dónde vive el WebSocket:**

- **`notificaciones`.** Ya consume `pedido.estado_cambiado` para los mails, así que era menos trabajo. Se descartó porque el WebSocket es seguimiento del pedido, no un aviso más, y porque `notificaciones` es la capacidad compartida con otros grupos: sumarle lógica de compradores la ata a nuestro dominio. Además, sus dos instancias comparten una cola y cada evento llega a una sola.
- **Un servicio nuevo, solo para el tiempo real.** Límites limpios, pero suma un servicio entero (Dockerfile, configuración, logs, métricas y trazas copiados según ADR-006) para algo que pertenece al pedido.
- **El api-gateway.** Ve todas las conexiones, pero pasaría a depender de RabbitMQ y a tener lógica de negocio, y ADR-007 lo define como un proxy sin dominio.

**Librería:**

- **`gorilla/websocket`.** La más conocida y con más ejemplos, pero sin `context`: los timeouts y el cierre se manejan a mano.
- **`golang.org/x/net/websocket`.** Su propia documentación recomienda usar otra, porque le faltan funciones como el cierre ordenado.

**Cómo se autentica:** las alternativas están en [ADR-019](ADR-019-sesion-en-cookie.md).

**Cómo llegan los cambios:**

- **Que `pedidos` consuma sus propios eventos desde RabbitMQ**, con una cola exclusiva. Son los mismos eventos que usan los mails, pero llegan con la demora de la bandeja de salida (alrededor de 1 s) y sin RabbitMQ no hay tiempo real. Con una sola instancia no aporta nada sobre avisar al guardar.
- **Que la pantalla consulte cada pocos segundos (*polling*).** Sin WebSocket, pero no cumple B2, que pide WebSocket, y no sirve para el mapa.

## Consecuencias

- `pedidos` suma una dependencia (`coder/websocket`), un adaptador de entrada y el puerto `AvisosEnVivo`, que la aplicación llama después de cada guardado con evento.
- El tiempo real no depende de RabbitMQ: con el broker caído, el comprador igual ve los cambios en vivo, y los mails salen cuando vuelve.
- Los compradores conectados viven en la memoria de `pedidos`: un reinicio corta las conexiones, y la pantalla se reconecta sola y vuelve a pedir el estado por HTTP.
- El gateway y el servidor del frontend necesitan un caso especial para esta ruta: sin el corte de 3 s y con el puente de WebSocket.
- Con el formato con `tipo`, sumar el mapa del delivery no cambia el protocolo. El estado `en_camino` (entre `listo` y `entregado`, opcional, lo provoca el vendedor) se agrega aparte: es el momento en que tendría sentido mandar la ubicación.
- Para los tests: el adaptador se prueba con un servidor de prueba y un cliente de `coder/websocket`, y el filtro por comprador, con eventos de dos compradores distintos.

## Validación (implementación, 06/11/2026)

Medido en el navegador con la app completa en Docker (#75):

- **Sin recargar:** con Mis pedidos abierta, cuando el vendedor pasa un pedido a `listo`, la pantalla lo muestra en unos 50 ms.
- **Solo sus pedidos:** un comprador conectado no recibió ningún mensaje del pedido de otro comprador.
- **Reconexión:** con la conexión cortada se cambió el estado del pedido; al volver, la pantalla se reconectó sola en menos de 1 s y mostró el estado actual.
- **Conexión larga:** después de 2,5 minutos sin actividad, la conexión seguía abierta y el siguiente cambio llegó igual.
- **Sin RabbitMQ:** con el broker detenido, el cambio llegó en vivo.
- **Sesión:** el WebSocket se abre con la cookie, y el token no aparece en los logs del api-gateway, de `pedidos` ni del servidor del frontend.
- **Desarrollo:** también funciona por el proxy de Vite, que no reenvía el `Origin`, porque `pedidos` rechaza un `Origin` distinto de su host.
