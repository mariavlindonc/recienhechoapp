# ADR-013 (D13): Capacidad y costos

## Estado

Propuesto (Entrega 2: plan de medición y límites esperados). Los tests de carga se corrieron el 29/10/2026 con la Decisión tal como está escrita; los resultados están en [Resultados](#resultados). La capacidad de notificaciones se despliega en Render, con su plan gratis: decidido por Agustín el 06/11/2026. Reemplaza al servidor propio con Cloudflare Tunnel, que la cátedra había aprobado el 29/10/2026.

## Contexto

El enunciado pide tests de carga con resultados documentados y analizados, el principal límite identificado, una alternativa de escalado y una estimación de costos. Los objetivos a verificar son:

| Objetivo | Origen |
|---|---|
| `POST /notificaciones`: 99 % en menos de 300 ms | SLO de [ADR-006](ADR-006-observabilidad.md) |
| Consultar el catálogo: p95 menor a 300 ms con caché | RNF2 |
| Crear un pedido: p95 menor a 2 s, incluida la llamada a inventario | RNF3 |
| Un cambio de producto aparece en la búsqueda en menos de 10 s | RNF1 |

## Decisión

**Herramienta: k6** (`grafana/k6`), corriendo en un contenedor contra el api-gateway. Los scripts quedan en el repositorio (`tests/carga/`) y los resultados se exportan a Prometheus para verlos en el mismo tablero que las métricas de los servicios.

**Escenarios:**

| Escenario | Qué hace | Carga |
|---|---|---|
| Notificaciones (Entrega 2) | `POST` con claves de idempotencia nuevas y `GET` de las creadas, con claves de API de varios grupos (el límite de 60 por minuto es por grupo) o con `NOTIFICACIONES_LIMITE_POR_MINUTO` más alto en el entorno de carga | Rampa de 10 a 200 usuarios virtuales en 5 min, después 5 min sostenido |
| Catálogo (presentación) | Listado por categoría y detalle, con caché apagada y prendida ([ADR-009](ADR-009-cache.md)) | Igual |
| Pedidos (presentación) | Crear pedidos para el mismo día hasta agotar el cupo, con el inventario falso en modo normal y lento | 50 usuarios virtuales |

En cada corrida se registra: solicitudes por segundo, p50/p95/p99, tasa de errores, CPU y memoria por contenedor, y en notificaciones cuántas quedan `pendiente` y cuánto tardan en enviarse.

**Carga esperada del negocio** (definida por María y Athina, 30/10/2026, [#79](https://github.com/mariavlindonc/recienhechoapp/issues/79)):

- **Carga real:** hasta **5 pedidos por día** en el pico del año. Es un emprendimiento real de una persona que cocina: el cuello de botella es la producción, no el sistema, y por eso existe el cupo diario del dominio.
- **Techo hipotético:** **50 pedidos por segundo**, exagerado a propósito, solo para tener un margen de seguridad con el que comparar las mediciones. No es una carga que se espere.
- Los escenarios de carga de abajo no representan esa demanda: buscan el punto de quiebre y prueban la concurrencia. Por ejemplo, el cupo de 500 del escenario de pedidos sirve para que 50 compradores peleen por el mismo día; no es un dato del negocio.

**Límites esperados:**

1. **Servidor de correo.** Gmail permite unos 500 mails por día ([ADR-005](ADR-005-envio-de-email.md)). La API puede aceptar muchas más notificaciones de las que se pueden enviar: el límite real del servicio está en el envío. Con Mailpit no aparece, por eso se mide aparte el ritmo de envío. Además, cada instancia envía de a una notificación por vez (revisa las pendientes cada 5 s y las manda en serie): con 200 ms por envío serían unas 5 por segundo por instancia, y el ritmo se multiplica con más instancias.
2. **MongoDB de una sola instancia.** Todas las instancias de `notificaciones` escriben en la misma base; con el volumen de un grupo consumidor no debería ser el límite, pero es el siguiente punto único de falla.
3. **Cupo de `pedidos`.** El update condicional sobre el documento del día serializa las reservas de ese día; es correcto por diseño y se mide cuánto tarda bajo contención.

**Alternativas de escalado:**

- `notificaciones`: más instancias detrás del gateway ([ADR-012](ADR-012-balanceo-de-carga.md)), solo cambiando `NOTIFICACIONES_URLS`. Para superar el límite de Gmail, pasar a un proveedor transaccional (Amazon SES, Brevo, Resend) cambiando las variables `SMTP_*`.
- MongoDB: réplica de 3 nodos para disponibilidad; sharding no hace falta con este volumen.
- `productos`: la caché absorbe las lecturas; si no alcanza, más instancias.

**Estimación de costos en la nube** (orden de magnitud, en dólares por mes). Los números no salen de precios publicados de ningún proveedor. La capacidad va en el plan gratis de Render y no tiene costo de nube; la tabla sirve para comparar con una VM.

| Escenario | Qué incluye | Costo estimado |
|---|---|---|
| Demo (lo que se despliega) | Una VM chica (2 vCPU, 4 GB) con `api-gateway`, 2 × `notificaciones`, MongoDB, Jaeger, Prometheus y Grafana en Docker; Gmail | 15 a 30 |
| Producción chica | 2 VMs para los servicios, MongoDB administrado de nivel básico, proveedor de mail transaccional | 60 a 120 |

El costo crece por el envío de mails y por la base administrada; el cómputo de los servicios en Go es lo más barato.

**Dónde desplegar:** en [Render](https://render.com/docs/free), con su plan gratis. El enunciado pide la capacidad propia en la nube con una URL pública, así que sube `notificaciones` con lo que necesita para arrancar; el resto del sistema sigue en el compose local. Límites del plan gratis (documentación de Render, consultada el 06/11/2026):

- Solo los web services tienen plan gratis, además de Postgres y Key Value. Los servicios privados y los workers son pagos, así que MongoDB y RabbitMQ no pueden correr gratis en Render.
- Un web service gratis se duerme después de 15 minutos sin tráfico entrante. Mientras duerme, `notificaciones` no envía las pendientes ni consume eventos.
- Hay 750 horas gratis por mes para todo el workspace. Un servicio prendido todo el mes usa unas 720; dos ya no entran.

La cátedra permitió reducir `notificaciones` a lo mínimo para que entre en el plan gratis, y alcanza con que funcione el día de la evaluación (consultado por Agustín, 06/11/2026). La versión en Render queda así:

- Dos web services gratis: el api-gateway adelante, como pide [ADR-004](ADR-004-contrato-de-notificaciones.md) (aplica el límite de 60 por minuto del contrato), y una sola instancia de `notificaciones`. El balanceo sobre dos instancias ([ADR-012](ADR-012-balanceo-de-carga.md)) se muestra en el compose local.
- Las notificaciones y las claves de idempotencia se guardan en el Key Value gratis de Render (`REDIS_URL` en lugar de `MONGO_URI`). Si Render lo reinicia, se pierden ([ADR-002](ADR-002-persistencia.md)).
- Sin RabbitMQ: sin `RABBITMQ_URL`, `notificaciones` atiende solo la API pública y no consume eventos de `pedidos`.
- El correo sale por Brevo en el puerto 2525, porque Render bloquea el SMTP en los puertos 25, 465 y 587 ([ADR-005](ADR-005-envio-de-email.md)).
- El gateway exige las URLs de `pedidos`, `productos` y `usuarios`. En Render apuntan a direcciones sin servicio y esas rutas responden `503`.
- Un web service gratis no recibe tráfico por la red privada de Render, así que el gateway llega a `notificaciones` por su URL pública. Esa URL también se puede llamar directo, sin pasar por el límite por minuto del gateway; igual pide `X-API-Key`.
- Los dos web services se despiertan juntos y gastan las 750 horas a la par: alcanzan si no quedan despiertos más de la mitad del mes. El chequeo de `/vivo` del gateway mantiene despierta a `notificaciones` solo mientras el gateway está despierto. Si los dos duermen, la primera solicitud despierta al gateway, que todavía no llega a `notificaciones` y responde `503` hasta que arranca; el consumidor reintenta con la misma `Idempotency-Key`. El día de la evaluación conviene despertarlos antes.
- El mock del contrato ([ADR-004](ADR-004-contrato-de-notificaciones.md)) también sube, como tercer web service gratis, para que el grupo consumidor pruebe sin pegarle al servicio real. Se arma con el contrato de la rama desplegada, así que no se desincroniza. Solo gasta horas cuando alguien lo usa.
- La configuración de Render está en [`render.yaml`](../../render.yaml) (Blueprint).

Las opciones anteriores eran un servidor propio con Cloudflare Tunnel (la elegida hasta el 06/11/2026), una VM en la nube con el compose tal como está y Cloudflare Containers (unos 5 USD por el mes de la demo, con MongoDB externo).

Sea cual sea el lugar, Grafana se publica por HTTPS, sin acceso anónimo y con una cuenta por persona: el equipo como Admin o Editor, la cátedra y el cliente como Viewer ([ADR-006](ADR-006-observabilidad.md)).

## Resultados

Corridos el 29/10/2026 con `tests/carga/` ([README](../../tests/carga/README.md); resultados crudos en [`tests/carga/resultados/2026-10-29`](../../tests/carga/resultados/2026-10-29/README.md)), en la computadora de desarrollo: todo el compose y k6 en Docker sobre WSL, en la misma máquina. Con k6 y los servicios compartiendo CPU, los números sirven para comparar y encontrar límites, no como capacidad de producción.

| Escenario | Carga | Solicitudes | p95 | p99 | Errores | Objetivo |
|---|---|---|---|---|---|---|
| Notificaciones | Rampa a 200 usuarios, 10 min | 182.126 (303 por s) | 3,39 ms (`POST`) | 4,64 ms (`POST`) | 0 % | SLO: 99 % en menos de 300 ms. **Cumple** |
| Catálogo con caché | Igual | 182.234 (303 por s) | 2,88 ms (listado), 2,33 ms (detalle) | — | 0 % | RNF2: p95 menor a 300 ms. **Cumple** |
| Catálogo sin caché | Igual | 181.886 (303 por s) | 4,63 ms (listado), 4,38 ms (detalle) | — | 0 % | Comparación de [ADR-009](ADR-009-cache.md) |
| Pedidos, inventario normal | 50 usuarios sin pausa, 2 min | 108.071 (863 por s) | 146 ms (todas), 300 ms (las `201`) | — | 4,5 % de `503` | RNF3: p95 menor a 2 s. **Cumple** |
| Pedidos, inventario lento | Igual | 103.169 (824 por s) | 159 ms (todas), 1,6 s (las `201`) | — | 1,9 % de `503` | RNF3. **Cumple**, cerca del límite en las `201` |

**Notificaciones:** se crearon 91.066 y se enviaron todas a Mailpit. Al terminar el test quedaban 132 pendientes, que salieron en los segundos siguientes. Entre que se crea y se envía, una notificación tardó 1,8 s en promedio (p95 de 3,6 s, máximo de 4,9 s), entre las dos instancias a unas 150 por segundo. CPU máximo: MongoDB 94 %, cada instancia de `notificaciones` 65 %, el gateway 34 %.

**Pedidos:** en las dos corridas se crearon exactamente 500 pedidos, el cupo del día. No hubo sobreventa con 50 usuarios a la vez: la reserva atómica de [ADR-015](ADR-015-consistencia-y-concurrencia.md) se cumple. El cupo se agota al principio del test; después, casi todas las respuestas son `409 sin_cupo`, en unos 40 ms. Con el inventario lento, crear un pedido tarda más (p95 de 1,6 s en las `201`) porque espera la primera confirmación hasta el timeout del inventario. Después responde con el pedido en `pendiente_confirmacion`, y sigue tomando pedidos (RNF4). Al volver el inventario a `confirma`, el proceso en segundo plano confirmó los 500 sin intervención. En esta corrida, el umbral de k6 todavía medía todas las respuestas, incluidos los `409`; el p95 de los pedidos creados de la tabla salió del resumen de k6. Ahora el umbral mide solo los creados.

**Capacidad contra la carga esperada:**

| | Carga real | Techo hipotético | Medido sin errores | Margen sobre el techo |
|---|---|---|---|---|
| Pedidos | 5 por día | 50 por segundo | ~300 por segundo (el límite de conexiones del gateway aparece a ~860) | ~6 veces |
| Catálogo | Unas pocas consultas por día | ~150 por segundo (3 consultas por pedido, supuesto) | 303 por segundo, 0 % de errores (no se buscó su límite) | al menos 2 veces |

Contra la carga real, el sistema sobra por muchos órdenes de magnitud. La arquitectura responde a los requisitos del Trabajo Práctico (microservicios, balanceo, mensajería, etc.), no a la carga del negocio.

**Límites esperados, contra lo medido:**

1. **Servidor de correo.** Con Mailpit, el envío no fue un límite: unas 150 notificaciones por segundo entre las dos instancias. La estimación de unas 5 por segundo por instancia suponía 200 ms por envío, y Mailpit tarda unos milisegundos. Con Gmail el límite sigue siendo el de unos 500 mails por día; no se midió.
2. **MongoDB de notificaciones.** Llegó al 94 % de CPU con 300 solicitudes por segundo. Es el siguiente límite de notificaciones si crece la carga.
3. **Cupo de `pedidos`.** Correcto bajo contención (500 de 500, sin sobreventa) y sin demoras visibles: el p95 de las `201` fue de 300 ms.

**Principal límite encontrado: las conexiones del api-gateway hacia los servicios.** Con 860 solicitudes por segundo a `pedidos`, el gateway respondió `503` a 6.908 solicitudes (4,5 % y 1,9 %). Todas fallaron con `dial tcp ...: connect: cannot assign requested address`: el contenedor del gateway se quedó sin puertos locales para abrir conexiones nuevas. El gateway usa el transporte HTTP por defecto de Go, que guarda solo 2 conexiones libres por servicio para reusarlas. Con 50 solicitudes a la vez, cada solicitud de más abre una conexión nueva y la cierra al terminar, y los puertos cerrados quedan unos segundos sin poder usarse (estado `TIME_WAIT`). Con notificaciones y catálogo, a 300 por segundo, no pasó. Se resolvería subiendo las conexiones libres que el gateway guarda por servicio (`MaxIdleConnsPerHost`).

**Qué se hace con ese límite:** se acepta como deuda técnica, sin subir `MaxIdleConnsPerHost` (decidido por María y Athina, 30/10/2026, [#79](https://github.com/mariavlindonc/recienhechoapp/issues/79)). Aparece a ~860 solicitudes por segundo, unas 17 veces por encima del techo hipotético de 50 pedidos por segundo y muchísimo más que la carga real. La causa está entendida y la solución es conocida (subir las conexiones libres que el gateway guarda por servicio), así que se puede aplicar si la carga alguna vez se acerca.

**Otras observaciones:**

- `pedidos` llegó a 14 núcleos de CPU y `productos` a entre 4 y 6: para cada pedido, incluso los que terminan en `409 sin_cupo`, `pedidos` pide el producto a `productos` antes de reservar el cupo.
- RabbitMQ usó entre 2 y 4 núcleos de CPU en todas las corridas, incluso en las del catálogo, que no publican eventos. No se investigó la causa.

## Alternativas consideradas

- **JMeter.** Muy completo, pero pesado y con scripts en XML difíciles de revisar en un PR.
- **Locust.** Scripts en Python, cómodo, pero suma un lenguaje más al proyecto.
- **`hey` o `ab`.** Sirven para una ruta suelta, pero no para escenarios con varios pasos ni rampas.

## Consecuencias

- k6 entra como herramienta de pruebas, fuera de los servicios.
- Las estimaciones de "Límites esperados" quedan como estaban; lo medido está en [Resultados](#resultados). Los costos siguen siendo estimaciones hasta elegir dónde desplegar.
- Si el SLO o los timeouts no se cumplen, se ajustan en [ADR-003](ADR-003-comunicacion-entre-servicios.md) y [ADR-006](ADR-006-observabilidad.md).
