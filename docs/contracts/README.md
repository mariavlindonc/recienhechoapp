# Contratos

## Capacidad publicada: Notificaciones

Recién hecho le ofrece a otros grupos el envío de notificaciones por email a sus usuarios. El consumidor pide el envío y se desentiende: nosotros lo hacemos en segundo plano, reintentamos si el servidor de correo falla y dejamos el resultado disponible para consultar.

| | |
|---|---|
| Contrato | [notificaciones.yaml](notificaciones.yaml) (OpenAPI 3.1) |
| Versión vigente | **1.0.0** |
| Estado | Mock disponible; implementación real pendiente (Entrega 2) |
| Decisión | [ADR-004](../adr/ADR-004-contrato-de-notificaciones.md) |

### Operaciones

| Operación | Para qué |
|---|---|
| `POST /notificaciones` | Pedir el envío. Responde `202` con el `id` y estado `pendiente`. |
| `GET /notificaciones/{id}` | Consultar el estado: `pendiente`, `enviada` o `fallida` (con `motivo`). |

Por ahora el único canal es `email`, y el cuerpo es texto plano de hasta 5000 caracteres.

### Autenticación

Todas las operaciones llevan el header `X-API-Key` con la clave que le entregamos a cada grupo. Cada grupo solo ve sus propias notificaciones: el `GET` de un `id` creado con otra clave responde `404`.

### Idempotencia

El `POST` exige el header `Idempotency-Key` con un valor único por envío (recomendamos un UUID). La clave se recuerda durante 24 horas:

- Mismo valor y mismo cuerpo: responde `202` con la notificación ya creada (en su estado actual) y no envía otra. Sirve para reintentar sin miedo a mandar el mail dos veces.
- Mismo valor y otro cuerpo: `409 conflicto_idempotencia`.

### Errores

Todos los errores tienen la forma `{"codigo": "...", "mensaje": "..."}`. El programa consumidor tiene que decidir según `codigo`; `mensaje` es para personas y puede cambiar.

| HTTP | `codigo` | Qué significa | Qué hacer |
|---|---|---|---|
| 400 | `solicitud_invalida` | El cuerpo o los headers no respetan el contrato | Corregir; no reintentar igual |
| 401 | `no_autenticado` | Falta `X-API-Key` o no es válida | Revisar la clave |
| 404 | `no_encontrada` | El `id` no existe para esta clave | No reintentar |
| 409 | `conflicto_idempotencia` | La `Idempotency-Key` ya se usó con otro cuerpo | Generar una clave nueva |
| 429 | `limite_superado` | Más de 60 solicitudes por minuto | Esperar `Retry-After` segundos |
| 503 | `no_disponible` | El servicio está caído o saturado | Reintentar con espera exponencial y **la misma** `Idempotency-Key` |

Recomendamos un timeout de 3 segundos del lado del consumidor. Si el `POST` se corta por timeout, no se sabe si quedó creado: reintentar con la misma clave lo resuelve.

### Probar contra el mock

```bash
cp .env.example .env
docker compose up -d notificaciones-mock
```

```bash
curl -i -X POST http://localhost:4010/notificaciones \
  -H "X-API-Key: prueba" \
  -H "Idempotency-Key: 2b0e8f4a-7c1d-4f3e-8a9b-1c2d3e4f5a6b" \
  -H "Content-Type: application/json" \
  -d '{"canal":"email","destinatario":"cliente@ejemplo.com","asunto":"Tu pedido está listo","cuerpo":"Ya podés retirarlo."}'
```

```bash
curl -H "X-API-Key: prueba" http://localhost:4010/notificaciones/9f1c2a7e-3b4d-4e8a-9c21-5d6f7a8b9c0d
```

El mock valida el pedido contra el contrato: sin `X-API-Key` responde `401` y con un cuerpo inválido responde `400`. Para forzar otras respuestas se usa el header `Prefer`:

- `Prefer: example=fallida`: el `GET` devuelve una notificación fallida.
- `Prefer: code=503`: devuelve el error indicado (sirve para probar el manejo de errores del consumidor).

El mock devuelve siempre los mismos ejemplos: no guarda estado, así que no se puede probar la idempotencia ni que el estado cambie.

### Versionado

- La versión vigente está en `info.version` del contrato, con versionado semántico (`MAYOR.MENOR.PARCHE`). Las URLs no llevan versión.
- **Cambios compatibles** (sube la versión menor): agregar operaciones, campos opcionales en el pedido, campos en la respuesta, valores nuevos de `canal`. El consumidor tiene que ignorar los campos que no conoce.
- **Cambios incompatibles** (sube la versión mayor): quitar o renombrar campos, volver obligatorio uno opcional, cambiar significados o códigos de error. Los evitamos; si hiciera falta uno, se acuerda antes con el grupo consumidor y se avisa con al menos 2 semanas de anticipación.
- Cada cambio se anota en el historial de abajo; las versiones anteriores del archivo quedan en el historial de git.

### Historial

| Versión | Fecha | Cambio |
|---|---|---|
| 1.0.0 | 2026-10-09 | Versión inicial: envío por email y consulta de estado |

## Capacidad consumida: Inventario

La publica otro grupo. Cuando tengamos su contrato, va a quedar documentado acá cómo lo usamos (decisión D9, Entrega 2).

## APIs HTTP internas

Pendiente: se documentan a medida que se implementan los servicios.

## Eventos de mensajería

Pendiente (ver [ADR-003](../adr/ADR-003-comunicacion-entre-servicios.md)). Eventos previstos: `pedido.confirmado` y `pedido.estado_cambiado`.
