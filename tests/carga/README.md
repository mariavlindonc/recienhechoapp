# Tests de carga

Escenarios de [ADR-013](../../docs/adr/ADR-013-capacidad-y-costos.md), escritos para k6. k6 corre en un contenedor del compose, con el perfil `carga`, y le pega al api-gateway por la red interna. Los resultados salen en la consola y van a Prometheus: se ven en Grafana, en los paneles "Test de carga".

| Archivo | Qué mide | Carga |
|---|---|---|
| `notificaciones.js` | `POST /notificaciones` con claves de idempotencia nuevas y `GET` de la creada. Umbral: 99 % de los `POST` en menos de 300 ms (SLO de ADR-006) | Rampa de 10 a 200 usuarios virtuales en 5 min, después 5 min sostenido |
| `catalogo.js` | Listado por categoría y detalle de producto. Umbral: p95 menor a 300 ms (RNF2). Se corre con la caché prendida y apagada | Igual |
| `pedidos.js` | Crea pedidos para el mismo día hasta agotar el cupo (`201` y después `409 sin_cupo`). Umbral: p95 de los pedidos creados menor a 2 s (RNF3), medido en `pedido_creado_duracion`, sin los `409`. Se corre con el inventario falso en `confirma` y en `lento` | 50 usuarios virtuales durante 2 min |

## Correr

Con la app levantada y los budines de ejemplo cargados:

1. En el `.env`, subir `NOTIFICACIONES_LIMITE_POR_MINUTO` (por ejemplo, a `1000000`): con el límite de 60 por minuto, casi todos los `POST` de notificaciones darían `429`. Volver a `60` al terminar.
2. Aplicar el cambio: `docker compose up -d api-gateway`.
3. Correr un escenario:

```bash
docker compose --profile carga run --rm k6 run --tag testid=notificaciones /carga/notificaciones.js
```

`testid` separa las corridas en el tablero. Para comparar la caché, correr `catalogo.js` dos veces: una con `CACHE_HABILITADA=true` y otra con `CACHE_HABILITADA=false`, aplicando cada cambio con `docker compose up -d productos`. Lo mismo para `pedidos.js` con `INVENTARIO_MODO=confirma` y `INVENTARIO_MODO=lento`, agregando `-e DIAS=3` a la segunda corrida para que use otro día con el cupo libre.

Para probar que un script anda, sin carga: `docker compose --profile carga run --rm k6 run --vus 1 --iterations 1 /carga/catalogo.js`.

Los resultados medidos y su análisis están en [ADR-013](../../docs/adr/ADR-013-capacidad-y-costos.md#resultados). Los resultados crudos de cada corrida se guardan en `resultados/<fecha>/` ([29/10/2026](resultados/2026-10-29/README.md)).

En `pedidos.js`, k6 cuenta los `409 sin_cupo` como fallas en `http_req_failed`, aunque son la respuesta correcta: lo que vale es el check `creado o sin cupo`.

`pedidos.js` corta antes de tocar nada si el día ya tiene reservas (repetirlo el mismo día no crearía ningún pedido y pasaría igual) o si ningún producto se puede encargar con `DIAS` de anticipación. En los dos casos dice qué hacer.
