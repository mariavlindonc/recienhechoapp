# ADR-013 (D13): Capacidad y costos

## Estado

Propuesto (Entrega 2: plan de medición y límites esperados). Se completa con resultados reales para la presentación.

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

**Dónde desplegar para la demo: posibilidades, sin decidir.** En las tres, la URL pública sale de un subdominio de un dominio propio de un integrante, que ya está en Cloudflare.

| Opción | Cómo | Costo | A favor | En contra |
|---|---|---|---|---|
| VM en la nube | El `docker compose` tal como está, en una VM chica | Según el proveedor (sin elegir) | No hay que cambiar nada del compose | Hay que elegir proveedor; con 1 GB de RAM puede quedar justo |
| Cloudflare Containers | Cada servicio como contenedor de Cloudflare, configurado con wrangler | 5 USD del plan Workers Paid por el mes de la demo, más el uso (para dos contenedores chicos, unos pocos dólares) | Nube real, escala a cero, el dominio ya está en la misma cuenta | El disco no es persistente: MongoDB tiene que ser externo; no está verificado que el contenedor pueda salir por SMTP a Gmail; hay que configurar cada servicio por separado |
| Servidor propio de un integrante | Una notebook con Ubuntu (unos 3 GB de RAM) que ya corre Docker, publicada con un Cloudflare Tunnel | 0 | Gratis y disponible ya; el túnel no abre puertos | Hay que confirmar con la cátedra que cuente como "desplegada en la nube"; la máquina también da DNS a la casa, así que hay que limitar la memoria de los contenedores; depende de la luz y del Wi-Fi de la casa |

Los costos de Cloudflare salen de su documentación de precios (octubre de 2026). El acceso a Containers se verificó con wrangler: la cuenta necesita el plan Workers Paid.

## Alternativas consideradas

- **JMeter.** Muy completo, pero pesado y con scripts en XML difíciles de revisar en un PR.
- **Locust.** Scripts en Python, cómodo, pero suma un lenguaje más al proyecto.
- **`hey` o `ab`.** Sirven para una ruta suelta, pero no para escenarios con varios pasos ni rampas.

## Consecuencias

- k6 entra como herramienta de pruebas, fuera de los servicios.
- Las cifras de este ADR son estimaciones hasta que se corran las pruebas; para la presentación se completan con los resultados, el límite medido y su análisis.
- Si el SLO o los timeouts no se cumplen, se ajustan en [ADR-003](ADR-003-comunicacion-entre-servicios.md) y [ADR-006](ADR-006-observabilidad.md).
