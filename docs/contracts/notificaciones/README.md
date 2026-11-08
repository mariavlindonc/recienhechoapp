# Contrato de notificaciones (capacidad publicada)

Recién hecho le ofrece a otros grupos el envío de notificaciones por email. El consumidor pide el envío y se desentiende: nosotros lo hacemos en segundo plano, reintentamos si el servidor de correo falla y dejamos el resultado disponible para consultar.

| | |
|---|---|
| Contrato | [notificaciones.yaml](notificaciones.yaml) (OpenAPI 3.1) |
| Versión vigente | **1.0.1** |
| URL pública | https://recienhecho-api-gateway.onrender.com |
| Mock público | https://recienhecho-notificaciones-mock.onrender.com |
| Decisión | [ADR-004](../../adr/ADR-004-contrato-de-notificaciones.md); despliegue en [ADR-013](../../adr/ADR-013-capacidad-y-costos.md) |

### Operaciones

| Operación | Para qué |
|---|---|
| `POST /notificaciones` | Pedir el envío. Responde `202` con el `id` y estado `pendiente`, y el header `Location` con la URL para consultarla. |
| `GET /notificaciones/{id}` | Consultar el estado: `pendiente`, `enviada` (con `enviadaEn`) o `fallida` (con `motivo`). |

El destinatario puede ser cualquier dirección de email; no tiene que ser un usuario de Recién hecho. El asunto y el cuerpo los decide el consumidor y se envían tal cual. Por ahora el único canal es `email`; el asunto tiene hasta 150 caracteres y el cuerpo es texto plano de hasta 5000.

### Autenticación

Todas las operaciones llevan el header `X-API-Key` con la clave que le entregamos a cada grupo por un canal privado. Cada grupo solo ve sus propias notificaciones: el `GET` de un `id` creado con otra clave responde `404`.

### Idempotencia

El `POST` exige el header `Idempotency-Key` con un valor único por envío (recomendamos un UUID). La clave se recuerda durante 24 horas:

- Mismo valor y mismo cuerpo: responde `202` con la notificación ya creada (en su estado actual) y no envía otra. Sirve para reintentar sin miedo a mandar el mail dos veces.
- Mismo valor y otro cuerpo: `409 conflicto_idempotencia`.

Se puede armar la clave con un identificador propio, siempre que cada notificación distinta tenga la suya: por ejemplo, `pedido-123-confirmado` y `pedido-123-entregado`.

### Errores

Todos los errores tienen la forma `{"codigo": "...", "mensaje": "..."}`. El programa consumidor tiene que decidir según `codigo`; `mensaje` es para personas y puede cambiar.

| HTTP | `codigo` | Qué significa | Qué hacer |
|---|---|---|---|
| 400 | `solicitud_invalida` | El cuerpo o los headers no respetan el contrato | Corregir; no reintentar igual |
| 401 | `no_autenticado` | Falta `X-API-Key` o no es válida | Revisar la clave |
| 404 | `no_encontrada` | El `id` no existe para esta clave | No reintentar |
| 409 | `conflicto_idempotencia` | La `Idempotency-Key` ya se usó con otro cuerpo | Generar una clave nueva |
| 429 | `limite_superado` | Más de 60 solicitudes por minuto del mismo grupo, sumando `POST` y `GET` | Esperar `Retry-After` segundos |
| 503 | `no_disponible` | El servicio está caído, saturado o arrancando | Reintentar con espera exponencial y **la misma** `Idempotency-Key` |

### Disponibilidad

El servicio corre en el plan gratis de Render:

- **Se duerme después de 15 minutos sin uso.** El primer pedido después de eso tarda hasta alrededor de un minuto, o responde `503` mientras el servicio arranca. Los siguientes responden normal.
- **Timeout y reintentos:** recomendamos un timeout de 3 segundos y reintentos con espera exponencial durante unos 2 minutos, siempre con la misma `Idempotency-Key`. Si el `POST` se corta por timeout, no se sabe si quedó creado: reintentar con la misma clave lo resuelve.
- **Las notificaciones se guardan hasta la evaluación.** Si el servicio se reinicia, el `GET` de una notificación vieja puede responder `404`.

### Probar contra la implementación real

Con la clave de su grupo en lugar de `<clave>`:

```bash
curl -i -X POST https://recienhecho-api-gateway.onrender.com/notificaciones \
  -H "X-API-Key: <clave>" \
  -H "Idempotency-Key: 2b0e8f4a-7c1d-4f3e-8a9b-1c2d3e4f5a6b" \
  -H "Content-Type: application/json" \
  -d '{"canal":"email","destinatario":"alguien@ejemplo.com","asunto":"Tu turno fue confirmado","cuerpo":"Te esperamos el lunes a las 10."}'
```

Con el `id` de la respuesta:

```bash
curl -H "X-API-Key: <clave>" https://recienhecho-api-gateway.onrender.com/notificaciones/<id>
```

Repetir el `POST` devuelve el mismo `id`; con la misma `Idempotency-Key` y otro cuerpo responde `409`; sin `X-API-Key` responde `401`.

### Probar contra el mock

El mock acepta cualquier valor en `X-API-Key`:

```bash
curl -i -X POST https://recienhecho-notificaciones-mock.onrender.com/notificaciones \
  -H "X-API-Key: prueba" \
  -H "Idempotency-Key: 2b0e8f4a-7c1d-4f3e-8a9b-1c2d3e4f5a6b" \
  -H "Content-Type: application/json" \
  -d '{"canal":"email","destinatario":"alguien@ejemplo.com","asunto":"Tu turno fue confirmado","cuerpo":"Te esperamos el lunes a las 10."}'
```

```bash
curl -H "X-API-Key: prueba" https://recienhecho-notificaciones-mock.onrender.com/notificaciones/9f1c2a7e-3b4d-4e8a-9c21-5d6f7a8b9c0d
```

El mock valida el pedido contra el contrato: sin `X-API-Key` responde `401` y con un cuerpo inválido responde `400`. Para forzar otras respuestas se usa el header `Prefer`:

- `Prefer: example=fallida`: el `GET` devuelve una notificación fallida.
- `Prefer: code=503`: devuelve el error indicado (sirve para probar el manejo de errores del consumidor).

El mock devuelve siempre los mismos ejemplos: no guarda estado, así que no se puede probar la idempotencia ni que el estado cambie. También se duerme a los 15 minutos sin uso.

### Versionado

- La versión vigente está en `info.version` del contrato, con versionado semántico (`MAYOR.MENOR.PARCHE`). Las URLs no llevan versión.
- **Correcciones de documentación** (sube el parche): cambios en descripciones, ejemplos o servidores que no cambian el comportamiento.
- **Cambios compatibles** (sube la versión menor): agregar operaciones, campos opcionales en el pedido, campos en la respuesta, valores nuevos de `canal`. El consumidor tiene que ignorar los campos que no conoce.
- **Cambios incompatibles** (sube la versión mayor): quitar o renombrar campos, volver obligatorio uno opcional, cambiar significados o códigos de error. Los evitamos; si hiciera falta uno, se acuerda antes con el grupo consumidor y se avisa con al menos 2 semanas de anticipación.
- Cada cambio se anota en el historial de abajo; las versiones anteriores del archivo quedan en el historial de git.

### Historial

| Versión | Fecha | Cambio |
|---|---|---|
| 1.0.1 | 2026-11-07 | URLs públicas de Render en `servers`; guía de uso con la URL pública, la disponibilidad del plan gratis y la regla de versión de parche. Sin cambios de comportamiento |
| 1.0.0 | 2026-10-09 | Versión inicial: envío por email y consulta de estado |

### Para el equipo de Recién hecho

En local, la implementación real va por el api-gateway en `http://localhost:8080` (`docker compose up --build -d`, con la clave de `.env`) y el mail aparece en Mailpit (http://localhost:8025). El mock local se levanta con `docker compose up -d notificaciones-mock`, en `http://localhost:4010`.
