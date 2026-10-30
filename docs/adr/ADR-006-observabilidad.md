# ADR-006 (D11): Observabilidad

## Estado

Aceptado (versión inicial, Entrega 2; se completa para la presentación con el tablero final y las alertas)

## Contexto

El enunciado pide poder entender el estado de una operación sin revisar cada componente a mano: logs estructurados, métricas y un tablero, y para la Entrega 2 logs correlacionados y una primera traza distribuida del flujo principal.

Hay tres señales y cada una responde una pregunta distinta:

- **Logs:** qué pasó exactamente en una solicitud.
- **Métricas:** si el sistema anda bien en general (solicitudes por segundo, errores, latencia).
- **Trazas:** por qué servicios pasó una solicitud y cuánto tardó cada tramo.

El primer flujo que funciona de punta a punta es el de la capacidad publicada: `POST /notificaciones` entra por el api-gateway, llega a `notificaciones`, se guarda en MongoDB y después se envía por SMTP ([ADR-005](ADR-005-envio-de-email.md)).

## Decisión

**Logs: `log/slog` de la librería estándar, en JSON a la salida estándar.**

- Cada línea lleva siempre `time`, `level`, `msg`, `servicio`, y `trace_id` y `span_id` cuando hay una traza activa.
- Un middleware registra cada solicitud HTTP: método, ruta, estado y duración.
- Los logs de distintos servicios que corresponden a la misma solicitud comparten el `trace_id`: así se correlacionan.
- Nunca se loguean secretos, claves de API ni cuerpos de mails.
- Por ahora se leen con `docker compose logs`.

**Trazas: OpenTelemetry (SDK de Go) exportando por OTLP directo a Jaeger.**

- OpenTelemetry es la librería que genera las trazas en un formato estándar (OTLP); Jaeger las guarda y las muestra en una web.
- Instrumentación: `otelgin` en la entrada HTTP de cada servicio, `otelhttp` en las llamadas salientes del api-gateway (propaga el header `traceparent`, que es lo que une los tramos de distintos servicios en una sola traza) y un monitor de comandos en el acceso a MongoDB.
- **Nota de implementación (Entrega 2):** la instrumentación oficial `otelmongo` no tiene versión publicada para `mongo-driver/v2` (solo un commit en desarrollo), así que `notificaciones` usa un monitor de comandos propio (`internal/trazas`) que crea un span por comando con la librería de trazas de OpenTelemetry. Además, solo traza los comandos que son parte de una solicitud con traza: el proceso de envío consulta MongoDB cada 5 s y, si no, llenaría Jaeger de trazas sueltas. Cuando `otelmongo` publique una versión para el driver v2, se puede reemplazar sin cambiar el resto.
- Los chequeos de salud (`/health` y `/vivo`) no se trazan: los consultan máquinas (el balanceador, un orquestador), cada pocos segundos y siempre igual, así que no aportan información y tapan las trazas útiles. La contra es que, si un día `/health` tarda (hace ping a MongoDB), no hay traza que muestre dónde; el log de la solicitud igual registra la duración.
- El envío SMTP, que pasa en segundo plano, abre su propio span vinculado a la traza de la solicitud que creó la notificación.
- El destino se configura con `OTEL_EXPORTER_OTLP_ENDPOINT`; el código no conoce a Jaeger.

**Métricas: Prometheus, y Grafana como tablero.**

- Cada servicio expone `/metrics` con el cliente oficial de Prometheus para Go: solicitudes por ruta y estado, latencia (histograma) y, en `notificaciones`, notificaciones por estado y reintentos de envío.
- Prometheus lee `/metrics` de cada servicio cada 15 s.
- Grafana usa Prometheus y Jaeger como fuentes de datos. Las fuentes y el tablero se cargan desde archivos del repositorio, sin configurarlos a mano.

**Acceso al tablero.** Lo van a usar el equipo, la cátedra y, cuando el sistema pase a ser un proyecto real, el cliente. Se distinguen dos entornos:

- **Desarrollo local** (el `docker compose up` de cada integrante): cada uno tiene su propia copia del sistema y su propio Grafana. Prometheus y Grafana publican sus puertos solo en `127.0.0.1`: se ven desde la propia computadora y no desde la red local (por ejemplo, el wifi de la facultad). Grafana permite entrar sin usuario, con permiso de solo lectura, porque solo llega quien está en esa computadora. Para mostrar el tablero en una reunión, se comparte pantalla.
- **Despliegue** (la demo en la nube y después el proyecto real): un Grafana compartido, **sin acceso anónimo**, con una cuenta por persona y un rol según quién es: Admin o Editor para el equipo, Viewer (solo lectura) para la cátedra y el cliente. Se entra por HTTPS. Así se puede dar y quitar acceso a cada persona sin compartir contraseñas.

**Objetivo de servicio (SLO) inicial:** el 99 % de los `POST /notificaciones` responden en menos de 300 ms, medido en el api-gateway sobre una ventana de 1 hora.

**Alerta inicial:** más de 5 notificaciones pasan a `fallida` en 10 minutos (el servidor de correo está caído o rechazando envíos).

## Convenciones compartidas entre servicios

Agregado el 22/10/2026, por decisión de Athina a partir de la revisión del código.

Como un servicio no importa código de otro (AGENTS.md), el código de logs, trazas y métricas está **copiado** en cada servicio. Las copias pueden diferir en su forma interna, pero tienen que respetar este contrato, porque el tablero, las alertas y la correlación de logs dependen de que todos los servicios lo cumplan igual:

- el panel de latencia consulta `duracion_solicitud_http_segundos` de todos los servicios;
- buscar los logs de una solicitud depende de que todos escriban `trace_id`.

Si una copia se desvía, esa parte deja de funcionar para ese servicio **sin dar ningún error**.

**Logs**
- Una línea JSON por evento en la salida estándar (`slog` con `JSONHandler`), con `time`, `level`, `msg`, `servicio`, y `trace_id` y `span_id` cuando hay una traza activa.
- Cada solicitud HTTP: `msg` "solicitud atendida", con `metodo`, `ruta` (la ruta con parámetros de Gin, `FullPath()`, o `sin_ruta` si no coincide ninguna), `estado` y `duracion_ms`.
- Nunca se registran claves de API, claves de idempotencia, destinatarios ni contenidos de mensajes. Los emails que aparezcan en un error se reemplazan por `[email oculto]`.

**Métricas**
- `/metrics` en un puerto interno (`METRICAS_PORT`, 9464) que no se publica fuera del compose, con un registro propio que también incluye las métricas del runtime de Go y del proceso.
- Nombres en español, en minúscula con guion bajo, terminados en la unidad o en `_total`. Etiquetas también en español.
- En todos los servicios: `solicitudes_http_total{metodo, ruta, estado}` y `duracion_solicitud_http_segundos{metodo, ruta}`, con los rangos `0.005, 0.01, 0.025, 0.05, 0.1, 0.3, 0.5, 1, 3, 5` (el 0,3 mide el objetivo de servicio). `ruta` usa `FullPath()`, nunca la ruta real, para no crear una serie por id.
- Los contadores que usa una alerta se crean en cero al arrancar, para que la serie exista antes del primer evento.

**Trazas**
- SDK de OpenTelemetry con exportador OTLP/HTTP hacia `OTEL_EXPORTER_OTLP_ENDPOINT`, propagador W3C (`traceparent` y `baggage`) y `service.name` igual al nombre del servicio.
- Sin `OTEL_EXPORTER_OTLP_ENDPOINT`, el servicio arranca igual y solo propaga la traza. Los errores de exportación van al log como `WARN`; nunca cortan una solicitud.
- Los chequeos de salud (`/health` y `/vivo`) no se trazan.

**Orden de los middlewares de Gin:** `otelgin` → `gin.Recovery()` → logs de solicitudes → métricas. `otelgin` va primero para que los demás ya vean el `trace_id`.

**Archivos copiados que tienen que mantenerse alineados**

| api-gateway (paquete `main`) | notificaciones | Qué tienen en común |
|---|---|---|
| `registro.go` (`nuevoLogger`, `registrarSolicitudes`) | `internal/registro` (`NuevoLogger`, `Solicitudes`) | Logs JSON con traza y log de cada solicitud |
| `trazas.go` (`iniciarTrazas`) | `internal/trazas` (`Iniciar`) | Configuración de OpenTelemetry. Notificaciones suma el monitor de MongoDB |
| `metricas.go` (parte HTTP) | `internal/adaptadores/metricas` | Métricas HTTP y servidor de `/metrics`. Cada uno suma las suyas |

Los nombres cambian porque en el gateway son funciones del paquete `main`, que no se exportan, y en notificaciones son paquetes propios. **Cambiar una convención es cambiarla en todos los servicios en el mismo PR y actualizar esta sección.** Los servicios nuevos copian de notificaciones.

## Alternativas consideradas

- **OpenTelemetry Collector entre los servicios y los backends.** Desacopla y permite filtrar o reenviar, pero es una pieza más para operar y entender. Como los servicios hablan OTLP estándar, se puede agregar después sin tocar código.
- **Grafana Tempo en lugar de Jaeger.** Se integra mejor con Grafana, pero necesita más configuración. Jaeger es un solo contenedor que recibe OTLP directamente y trae su propia interfaz.
- **Imagen todo-en-uno `grafana/otel-lgtm`.** Se levanta rápido, pero esconde las piezas y es más difícil de explicar qué hace cada una.
- **Loki (o ELK) para centralizar logs.** Es útil para buscar logs desde Grafana, pero para la Entrega 2 alcanza con `docker compose logs`. Se evalúa para la presentación.
- **Puertos de observabilidad abiertos a la red local en desarrollo** (lo que hace Docker por defecto). Un compañero en la misma red podría abrir el tablero desde su computadora, pero también cualquier otra persona de esa red, sin usuario. No resuelve el acceso compartido, porque cada integrante tiene su propia copia de los datos.
- **Grafana Cloud** (servicio administrado con plan gratuito) para el despliegue. No hay que operar Grafana y las cuentas se invitan por mail, pero suma un proveedor externo y los datos salen del sistema. Se puede reconsiderar si operar Grafana resulta costoso.
- **Tablero público de solo lectura** (función *public dashboard* de Grafana: un link sin login). Es lo más cómodo para mostrarle el sistema a la cátedra o al cliente, pero quien tenga el link ve el tablero y no hay control por persona.
- **Un módulo común (`backend/comun`) con el código de logs, trazas y métricas** en lugar de copias. Evita que las copias diverjan, pero acopla la construcción de los servicios (hoy cada imagen se construye solo con la carpeta de su servicio) y se discutió frente a la regla de no importar código de otro servicio. Se eligió mantener las copias y escribir el contrato de arriba.
- **Logs con zap o zerolog.** Son más rápidos, pero `slog` viene con Go y alcanza para este volumen.

## Consecuencias

- Se suman tres contenedores al `docker-compose`: Jaeger, Prometheus y Grafana.
- Cada servicio suma dependencias de OpenTelemetry y del cliente de Prometheus.
- Mientras los logs no estén centralizados, para seguir una solicitud hay que buscar el `trace_id` en los logs de cada contenedor.
- Jaeger guarda las trazas en memoria: se pierden al reiniciarlo. Alcanza para desarrollo y la demo. Guarda hasta 20.000 y borra las más viejas, para no pasar de unos 250 MB de RAM.
- El SLO y el umbral de la alerta son iniciales: se ajustan con los tests de carga (D13).
- En desarrollo, el tablero no se puede ver desde otra computadora: para mostrarlo hay que compartir pantalla.
- En el despliegue hay que administrar las cuentas de Grafana (alta, rol y baja de cada persona) y configurar HTTPS ([ADR-013](ADR-013-capacidad-y-costos.md)).
