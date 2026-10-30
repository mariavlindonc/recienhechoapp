# Postmortems

Informes de las caídas controladas que se ensayaron sobre el sistema, como pide [ADR-011](adr/ADR-011-resiliencia.md). Todas se provocaron a propósito en la computadora de desarrollo, con el compose completo. Los horarios son de Buenos Aires (GMT-3).

1. [Una instancia de notificaciones caída](#1-una-instancia-de-notificaciones-caída) (29/10/2026)
2. [Servidor de correo caído](#2-servidor-de-correo-caído) (22/10/2026)
3. [Inventario del proveedor lento](#3-inventario-del-proveedor-lento) (29/10/2026)

## 1. Una instancia de notificaciones caída

### Resumen

Con tráfico continuo a `POST /notificaciones`, se detuvo `notificaciones-1`, una de las dos instancias que reparte el api-gateway ([ADR-012](adr/ADR-012-balanceo-de-carga.md)). El gateway la sacó del balanceo con la primera solicitud que falló y mandó el resto a `notificaciones-2`. Al prenderla de nuevo, volvió sola en el chequeo siguiente. Con las dos detenidas, el gateway respondió `503 no_disponible`.

### Impacto

- Con una instancia caída: 1 de 20 solicitudes respondió `503`. Las otras 19 respondieron `202`.
- Con las dos caídas: las 4 solicitudes respondieron `503 no_disponible`, que es lo que define el contrato ([ADR-003](adr/ADR-003-comunicacion-entre-servicios.md)).
- No se perdió ninguna notificación aceptada: los 35 mails de las solicitudes con `202` llegaron a Mailpit.

### Línea de tiempo

El tráfico fue de un `POST /notificaciones` cada 0,25 s, con claves de idempotencia nuevas. Las horas con segundos decimales salen de los logs del gateway; las que llevan "~" son aproximadas, porque se tomaron del script del ensayo.

| Hora | Qué pasó |
|---|---|
| ~20:46:33 | Las dos instancias están en el balanceo (`gateway_instancia_sana` = 1 en las dos). 8 de 8 solicitudes responden `202` |
| ~20:46:36 | `docker compose stop notificaciones-1` |
| 20:46:36.9 | La primera solicitud que le toca a `notificaciones-1` falla (`dial tcp: lookup notificaciones-1`) y responde `503`. El gateway la saca del balanceo sin esperar al chequeo: log `instancia fuera del balanceo`, con el motivo `falló un reenvío` |
| ~20:46:37 a ~20:46:42 | Las 19 solicitudes siguientes van a `notificaciones-2` y responden `202`. La métrica muestra `notificaciones-1` en 0 |
| ~20:46:42 | `docker start recienhecho-notificaciones-1` |
| 20:46:44.9 | El chequeo de `/vivo` la encuentra: log `instancia de vuelta en el balanceo`, con el motivo `respondió el chequeo`. 8 de 8 solicitudes responden `202`, repartidas entre las dos |
| ~20:46:57 | Se detienen las dos instancias. Los dos primeros reenvíos fallan y las sacan del balanceo |
| 20:46:58 | Log `no hay instancias sanas`: el gateway responde `503 no_disponible` sin intentar reenviar |
| 20:46:59.9 | Se prenden las dos y vuelven al balanceo en el chequeo siguiente |

### Causa raíz

Provocada: se detuvo el contenedor de una instancia. El gateway no reintenta un reenvío fallido (ADR-003), así que la solicitud que detecta la caída responde `503`. El consumidor la reintenta con la misma `Idempotency-Key`, como indica el contrato, y el reintento ya va a la otra instancia.

### Resolución

No hizo falta intervenir. El chequeo pasivo (un reenvío fallido) sacó a la instancia, y el activo (`GET /vivo` cada 5 s) la devolvió.

### Lecciones aprendidas

- El chequeo pasivo reduce el impacto a una sola solicitud. Con solo el chequeo activo, harían falta dos chequeos fallidos seguidos (hasta 10 s), y a 4 solicitudes por segundo, la mitad de esas solicitudes iría a la instancia caída.
- La alerta `ServicioSinMetricas` no se disparó: pide 1 minuto sin métricas y la instancia estuvo detenida unos 8 s. El panel "Instancias de notificaciones en el balanceo" sí mostró la caída.
- El ensayo mostró un error en los logs: la clave `servicio` salía repetida, una vez con `api-gateway` y otra con el servicio de destino. Se corrigió: el destino ahora va en la clave `destino`.
- Una revisión posterior encontró otro error, que el ensayo no cubría: si un cliente cortaba la solicitud por su propio plazo, el gateway sacaba del balanceo a una instancia sana. Se corrigió y tiene test.

### Acciones

| Acción | Estado |
|---|---|
| Clave `destino` en los logs del gateway | Hecha (PR #67) |
| Un cliente que corta no saca a la instancia del balanceo | Hecha, con test (PR #67) |
| Publicar la métrica inicial de salud sin pisar una falla del arranque | Hecha, con test (PR #67) |
| Confirmar los tres detalles del balanceo que aplicó el agente (ADR-012) | Pendiente: lo decide el equipo |

## 2. Servidor de correo caído

### Resumen

Se detuvo Mailpit, el servidor de correo de desarrollo ([ADR-005](adr/ADR-005-envio-de-email.md)), con notificaciones en cola. `POST /notificaciones` siguió respondiendo `202` y las notificaciones quedaron `pendiente`. El disyuntor del correo se abrió a los ~10 s y dejó de intentar envíos. Al volver Mailpit, salieron todas.

### Impacto

- Ningún consumidor recibió un error: los `POST` siguieron respondiendo `202`.
- Los mails se demoraron mientras Mailpit estuvo caído. Ninguna notificación quedó `fallida`.

### Línea de tiempo

Medido el 22/10/2026 con 60 notificaciones. De una ráfaga de 100, el límite de 60 por minuto rechazó 40 con `429`.

| Momento | Qué pasó |
|---|---|
| Inicio | `docker compose stop mailpit` y ráfaga de notificaciones |
| ~10 s | El disyuntor se abre después de 5 fallos seguidos |
| Mientras está caído | Un intento de prueba cada ~35 s (30 s abierto más hasta 5 s del sondeo). En total hubo 6 intentos SMTP: los 5 que lo abrieron y 1 prueba |
| Al volver Mailpit | La prueba del estado semiabierto cierra el circuito. Las 59 pendientes salen en 32 s |

### Causa raíz

Provocada: se detuvo el servidor de correo. `notificaciones` responde `202` sin esperar al envío ([ADR-004](adr/ADR-004-contrato-de-notificaciones.md)), así que la caída del correo no llega al consumidor.

### Resolución

Automática, al volver Mailpit.

### Lecciones aprendidas

- En el ensayo anterior, sin el disyuntor, cada notificación se reintentaba cada ~3 s y gastaba sus reintentos. La alerta tardaba de 15 a 20 minutos, y vaciar la cola, varios minutos más. Con el disyuntor, las pendientes no gastan reintentos y hay un solo intento cada ~35 s ([ADR-011](adr/ADR-011-resiliencia.md)).
- Una pendiente que no se puede enviar en 24 horas pasa a `fallida`, para que no quede pendiente para siempre.

### Acciones

| Acción | Estado |
|---|---|
| Disyuntor del correo con `sony/gobreaker` | Hecha (PR #57) |

## 3. Inventario del proveedor lento

### Resumen

Durante el test de carga de pedidos ([ADR-013](adr/ADR-013-capacidad-y-costos.md#resultados)), el inventario falso corrió en modo `lento`: tarda 5 s, más que el timeout de 1,5 s ([ADR-010](adr/ADR-010-consumo-de-inventario.md)). `pedidos` siguió tomando pedidos: los dejó en `pendiente_confirmacion` y los confirmó solo cuando el inventario volvió a responder.

### Impacto

- Crear un pedido tardó más: p95 de 1,6 s en los pedidos creados, contra 300 ms con el inventario normal. Sigue debajo de los 2 s de RNF3.
- Se crearon 500 pedidos, el cupo del día, sin sobreventa.
- El 1,9 % de las solicitudes respondió `503`. No fue por el inventario: el api-gateway se quedó sin puertos hacia `pedidos` con unas 820 solicitudes por segundo. Es el límite principal que encontró el test de carga, y también pasó con el inventario normal (4,5 %).

### Línea de tiempo

| Hora | Qué pasó |
|---|---|
| 21:47:33 | `pedidos` arranca con `INVENTARIO_MODO=lento` y 50 usuarios virtuales empiezan a crear pedidos para un mismo día, con cupo de 500 |
| Al principio del test | Se agota el cupo. Los pedidos creados esperan la confirmación hasta el timeout del inventario y quedan `pendiente_confirmacion`. El resto responde `409 sin_cupo` |
| 21:49:40 | Termina el test y `pedidos` vuelve a `INVENTARIO_MODO=confirma` |
| Después | El proceso en segundo plano retoma los 500 pedidos pendientes y los confirma. Todos quedaron `confirmado` |

### Causa raíz

Provocada: el inventario falso tarda más que el timeout. Mientras no llegue el contrato del grupo proveedor (D9), el inventario real no se puede probar.

### Resolución

Automática: la reconfirmación en segundo plano confirmó los pendientes cuando el inventario volvió a responder.

### Lecciones aprendidas

- Con el inventario lento se cumple RNF4: el sistema sigue tomando pedidos.
- El ensayo no midió cuánto tardó la reconfirmación en confirmar los 500 pedidos, ni si el disyuntor del inventario llegó a abrirse. Hay que medirlo en un ensayo dedicado.

### Acciones

| Acción | Estado |
|---|---|
| Ensayo dedicado de la caída del inventario, midiendo el disyuntor y la reconfirmación | Pendiente |
| Repetir el ensayo con el inventario real cuando llegue el contrato (D9) | Pendiente: depende del grupo proveedor |
| Decidir si se suben las conexiones que el gateway reusa por servicio (`MaxIdleConnsPerHost`) | Pendiente: lo decide el equipo ([ARCHITECTURE](ARCHITECTURE.md#limitaciones-conocidas-y-deuda-técnica)) |
