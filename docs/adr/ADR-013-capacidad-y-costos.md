# ADR-013 (D13): Capacidad y costos

## Estado

Propuesto (Entrega 2: plan de medición y límites esperados). Los tests de carga se corrieron el 29/10/2026 con la Decisión tal como está escrita; los resultados están en [Resultados](#resultados). Falta elegir dónde desplegar la demo.

El agente de código aplicó estos detalles, que la Decisión no define, y el equipo tiene que confirmarlos:

- **Versión de k6:** `grafana/k6:2.2.0`, la más nueva con al menos dos semanas publicada (AGENTS.md). Corre en el compose con el perfil `carga`, así que no arranca con el resto.
- **Límite de 60 por minuto:** de las dos opciones de la tabla, se usa `NOTIFICACIONES_LIMITE_POR_MINUTO` alto en el entorno de carga. Con varias claves harían falta unos 150 grupos para no chocar con el límite. Se sumó la variable a `.env.example` (60) y al compose.
- **Pausa de 1 s** entre iteraciones en notificaciones y catálogo, como un usuario que lee la pantalla. En pedidos no hay pausa: el escenario es agotar el cupo.
- **"El mismo día" de pedidos** es pasado mañana, porque los budines de ejemplo piden un día de anticipación (R2). Cupo de 500 y 2 minutos por corrida, con otro día en cada una.
- **Resultados en Prometheus:** Prometheus arranca con el receptor de escritura remota, k6 manda percentiles p50, p95 y p99 sin la URL como etiqueta (con el id, cada solicitud sería una serie), y el tablero suma dos paneles de "Test de carga".

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

**Límites esperados (a confirmar con los resultados):**

1. **Servidor de correo.** Gmail permite unos 500 mails por día ([ADR-005](ADR-005-envio-de-email.md)). La API puede aceptar muchas más notificaciones de las que se pueden enviar: el límite real del servicio está en el envío. Con Mailpit no aparece, por eso se mide aparte el ritmo de envío. Además, cada instancia envía de a una notificación por vez (revisa las pendientes cada 5 s y las manda en serie): con 200 ms por envío serían unas 5 por segundo por instancia, y el ritmo se multiplica con más instancias.
2. **MongoDB de una sola instancia.** Todas las instancias de `notificaciones` escriben en la misma base; con el volumen de un grupo consumidor no debería ser el límite, pero es el siguiente punto único de falla.
3. **Cupo de `pedidos`.** El update condicional sobre el documento del día serializa las reservas de ese día; es correcto por diseño y se mide cuánto tarda bajo contención.

**Alternativas de escalado:**

- `notificaciones`: más instancias detrás del gateway ([ADR-012](ADR-012-balanceo-de-carga.md)), solo cambiando `NOTIFICACIONES_URLS`. Para superar el límite de Gmail, pasar a un proveedor transaccional (Amazon SES, Brevo, Resend) cambiando las variables `SMTP_*`.
- MongoDB: réplica de 3 nodos para disponibilidad; sharding no hace falta con este volumen.
- `productos`: la caché absorbe las lecturas; si no alcanza, más instancias.

**Estimación de costos en la nube** (orden de magnitud, en dólares por mes). **Estimación a confirmar:** los números no salen de precios publicados de ningún proveedor; se confirman cuando el grupo elija dónde desplegar `notificaciones`.

| Escenario | Qué incluye | Costo estimado |
|---|---|---|
| Demo (lo que se despliega) | Una VM chica (2 vCPU, 4 GB) con `api-gateway`, 2 × `notificaciones`, MongoDB, Jaeger, Prometheus y Grafana en Docker; Gmail | 15 a 30 |
| Producción chica | 2 VMs para los servicios, MongoDB administrado de nivel básico, proveedor de mail transaccional | 60 a 120 |

El costo crece por el envío de mails y por la base administrada; el cómputo de los servicios en Go es lo más barato.

**Dónde desplegar la demo (sin decidir):**

- VM en la nube con el compose tal como está.
- Cloudflare Containers: unos 5 USD por el mes de la demo. MongoDB tendría que ser externo.
- Servidor propio con Cloudflare Tunnel: gratis. Falta confirmar con la cátedra que cuente como nube.

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

**Pedidos:** en las dos corridas se crearon exactamente 500 pedidos, el cupo del día. No hubo sobreventa con 50 usuarios a la vez: la reserva atómica de [ADR-015](ADR-015-consistencia-y-concurrencia.md) se cumple. El cupo se agota al principio del test; después, casi todas las respuestas son `409 sin_cupo`, en unos 40 ms. Con el inventario lento, crear un pedido tarda más (p95 de 1,6 s en las `201`) porque espera la primera confirmación hasta el timeout del inventario. Después responde con el pedido en `pendiente_confirmacion`, y sigue tomando pedidos (RNF4). Al volver el inventario a `confirma`, el proceso en segundo plano confirmó los 500 sin intervención.

**Límites esperados, contra lo medido:**

1. **Servidor de correo.** Con Mailpit, el envío no fue un límite: unas 150 notificaciones por segundo entre las dos instancias. La estimación de unas 5 por segundo por instancia suponía 200 ms por envío, y Mailpit tarda unos milisegundos. Con Gmail el límite sigue siendo el de unos 500 mails por día; no se midió.
2. **MongoDB de notificaciones.** Llegó al 94 % de CPU con 300 solicitudes por segundo. Es el siguiente límite de notificaciones si crece la carga.
3. **Cupo de `pedidos`.** Correcto bajo contención (500 de 500, sin sobreventa) y sin demoras visibles: el p95 de las `201` fue de 300 ms.

**Principal límite encontrado: las conexiones del api-gateway hacia los servicios.** Con 860 solicitudes por segundo a `pedidos`, el gateway respondió `503` a 6.908 solicitudes (4,5 % y 1,9 %). Todas fallaron con `dial tcp ...: connect: cannot assign requested address`: el contenedor del gateway se quedó sin puertos locales para abrir conexiones nuevas. El gateway usa el transporte HTTP por defecto de Go, que guarda solo 2 conexiones libres por servicio para reusarlas. Con 50 solicitudes a la vez, cada solicitud de más abre una conexión nueva y la cierra al terminar, y los puertos cerrados quedan unos segundos sin poder usarse (estado `TIME_WAIT`). Con notificaciones y catálogo, a 300 por segundo, no pasó. Se resolvería subiendo las conexiones libres que el gateway guarda por servicio (`MaxIdleConnsPerHost`). Esto no lo define ningún ADR: está pendiente de que el equipo decida.

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
