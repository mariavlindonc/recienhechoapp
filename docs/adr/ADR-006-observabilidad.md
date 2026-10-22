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
- `/health` no se traza: lo consulta el compose seguido y no aporta nada.
- El envío SMTP, que pasa en segundo plano, abre su propio span vinculado a la traza de la solicitud que creó la notificación.
- El destino se configura con `OTEL_EXPORTER_OTLP_ENDPOINT`; el código no conoce a Jaeger.

**Métricas: Prometheus, y Grafana como tablero.**

- Cada servicio expone `/metrics` con el cliente oficial de Prometheus para Go: solicitudes por ruta y estado, latencia (histograma) y, en `notificaciones`, notificaciones por estado y reintentos de envío.
- Prometheus lee `/metrics` de cada servicio cada 15 s.
- Grafana usa Prometheus y Jaeger como fuentes de datos. Las fuentes y el tablero se cargan desde archivos del repositorio, sin configurarlos a mano.

**Objetivo de servicio (SLO) inicial:** el 99 % de los `POST /notificaciones` responden en menos de 300 ms, medido en el api-gateway sobre una ventana de 1 hora.

**Alerta inicial:** más de 5 notificaciones pasan a `fallida` en 10 minutos (el servidor de correo está caído o rechazando envíos).

## Alternativas consideradas

- **OpenTelemetry Collector entre los servicios y los backends.** Desacopla y permite filtrar o reenviar, pero es una pieza más para operar y entender. Como los servicios hablan OTLP estándar, se puede agregar después sin tocar código.
- **Grafana Tempo en lugar de Jaeger.** Se integra mejor con Grafana, pero necesita más configuración. Jaeger es un solo contenedor que recibe OTLP directamente y trae su propia interfaz.
- **Imagen todo-en-uno `grafana/otel-lgtm`.** Se levanta rápido, pero esconde las piezas y es más difícil de explicar qué hace cada una.
- **Loki (o ELK) para centralizar logs.** Es útil para buscar logs desde Grafana, pero para la Entrega 2 alcanza con `docker compose logs`. Se evalúa para la presentación.
- **Logs con zap o zerolog.** Son más rápidos, pero `slog` viene con Go y alcanza para este volumen.

## Consecuencias

- Se suman tres contenedores al `docker-compose`: Jaeger, Prometheus y Grafana.
- Cada servicio suma dependencias de OpenTelemetry y del cliente de Prometheus.
- Mientras los logs no estén centralizados, para seguir una solicitud hay que buscar el `trace_id` en los logs de cada contenedor.
- Jaeger guarda las trazas en memoria: se pierden al reiniciarlo. Alcanza para desarrollo y la demo.
- El SLO y el umbral de la alerta son iniciales: se ajustan con los tests de carga (D13).
