# ADR-004 (D8): Contrato propio: notificaciones

## Estado

Aceptado

## Contexto

Cada grupo tiene que publicar una capacidad para que la use otro grupo. Esa capacidad necesita un contrato formal, versionado y en un formato estándar y procesable, guardado en `docs/contracts/`. Para la Entrega 1 el contrato va con un mock, así el grupo consumidor puede empezar a integrarse antes de que exista la implementación.

Publicamos **notificaciones**: otro sistema nos pide mandar un email a uno de sus usuarios y después consulta si salió. Nosotros ya lo necesitamos para avisarles a nuestros compradores, así que la capacidad sale de un servicio real del sistema.

## Decisión

- **Estilo:** API REST sobre HTTP/JSON.
- **Formato del contrato:** OpenAPI 3.1, en [`docs/contracts/notificaciones.yaml`](../contracts/notificaciones.yaml). El enunciado no impone un formato; elegimos OpenAPI porque es el estándar para describir APIs HTTP y porque hay herramientas que lo leen directamente (mocks, validadores, generadores de clientes).
- **Operaciones:** `POST /notificaciones` (responde `202` y el envío sigue en segundo plano) y `GET /notificaciones/{id}` (estado `pendiente`, `enviada` o `fallida`).
- **Asíncrono hacia adentro:** el `POST` no espera al servidor de correo. Así una demora del correo no le genera timeouts al consumidor, y los reintentos son responsabilidad nuestra.
- **Idempotencia:** el header `Idempotency-Key` es obligatorio en el `POST`. Misma clave y mismo cuerpo devuelven la notificación existente; misma clave y otro cuerpo, `409`. Así el consumidor puede reintentar ante un timeout sin mandar el mail dos veces.
- **Autenticación:** una clave por grupo en `X-API-Key`. Cada grupo solo ve sus notificaciones.
- **Errores:** un único formato `{codigo, mensaje}`, con códigos estables documentados.
- **Límite de uso:** 60 solicitudes por minuto por clave (`429` con `Retry-After`).
- **Exposición:** a través del api-gateway, el único punto de entrada del sistema.
- **Versionado:** versionado semántico en `info.version` del contrato, sin versión en la URL. Solo hacemos cambios compatibles (suben la versión menor); uno incompatible subiría la versión mayor y se acordaría antes con el grupo consumidor. Reglas completas en el [README de contratos](../contracts/README.md#versionado).
- **Mock:** [Prism](https://stoplight.io/open-source/prism) (`stoplight/prism:5`) levanta el contrato como servidor falso (`docker compose up -d notificaciones-mock`, puerto 4010). Valida los pedidos contra el contrato y responde con los ejemplos del YAML. Corre con `-m false` porque la versión 5 falla al arrancar con varios procesos.

## Alternativas consideradas

- **Eventos (AsyncAPI):** el otro grupo publica en una cola nuestra. Obliga a exponer el broker en internet y a darle credenciales a otro grupo, y es más difícil de probar. Lo descartamos para la capacidad externa; adentro del sistema sí usamos eventos ([ADR-003](ADR-003-comunicacion-entre-servicios.md)).
- **POST síncrono que espera el envío:** más simple para el consumidor, pero le traslada nuestras demoras y fallas del correo.
- **Versión en la URL:** permite convivir dos versiones a la vez, pero con un solo consumidor y un contrato chico no lo necesitamos y ensucia todas las rutas.
- **Versionado por header** (`Accept: application/vnd...`): menos visible y más difícil de probar con `curl`.
- **Mock escrito en Go:** no suma una herramienta, pero hay que mantenerlo a mano a la par del contrato; Prism lee el mismo archivo y no se puede desincronizar.

## Consecuencias

- El consumidor tiene que hacer polling del `GET` si quiere saber el resultado final; un webhook de aviso se puede sumar después como cambio compatible.
- Tenemos que guardar las claves de idempotencia (24 h) y las notificaciones por grupo ([ADR-002](ADR-002-persistencia.md)).
- El mock no tiene estado: sirve para integrar formatos y errores, no para probar idempotencia.
- Queda pendiente elegir el proveedor real de email (Entrega 2) y publicar la capacidad en una URL pública antes de la presentación.
