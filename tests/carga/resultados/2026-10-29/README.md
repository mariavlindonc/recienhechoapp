# Corrida del 29/10/2026

Resultados crudos de los tests de carga que se analizan en [ADR-013](../../../../docs/adr/ADR-013-capacidad-y-costos.md#resultados) y [ADR-009](../../../../docs/adr/ADR-009-cache.md). Se corrieron de 21:14 a 21:50 (Buenos Aires), en la computadora de desarrollo: el compose completo y k6 en Docker sobre WSL, en la misma máquina.

| Archivo | Qué tiene |
|---|---|
| `<escenario>.txt` | El resumen que imprime k6 al terminar: umbrales, checks y latencias por ruta |
| `<escenario>-recursos.txt` | `docker stats` de cada contenedor cada 15 s: CPU (100 % es un núcleo) y memoria |
| `notificaciones-envios-al-terminar.txt` | Notificaciones de la corrida por estado, apenas terminó el test, y la demora entre que se crearon y se enviaron |
| `notificaciones-envios-final.txt` | Lo mismo, con la cola ya vacía |
| `notificaciones-cola.txt` | Cuántas quedaban `pendiente`, consultado cada 10 s hasta vaciarse |

Escenarios, en orden: `notificaciones`, `catalogo-con-cache`, `catalogo-sin-cache` (con `CACHE_HABILITADA=false`), `pedidos-confirma` y `pedidos-lento` (con `INVENTARIO_MODO=lento`).

Para leerlos:

- Las horas dentro de los archivos están en UTC, como las imprime Docker: hay que restarles 3 horas.
- En los escenarios de pedidos, `http_req_failed` da 99 % porque k6 cuenta como falla toda respuesta 4xx, y casi todas son `409 sin_cupo`, que es la respuesta correcta cuando se agota el cupo. Lo que vale es el check `creado o sin cupo`: lo que no pasa son los `503` del gateway.
- En `pedidos-lento`, la fila `{ expected_response:true }` son solo los pedidos creados (`201`): su p95 de 1,6 s es la espera de la primera confirmación hasta el timeout del inventario.
