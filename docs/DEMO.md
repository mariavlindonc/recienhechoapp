# Guía de la presentación

Cómo levantar y mostrar Recién hecho en la presentación grupal (viernes 13 de noviembre), en una PC con Windows, PowerShell 7 y Docker Desktop. Todo se maneja con el panel de comandos [`demo.ps1`](../demo.ps1), desde la raíz del repo:

```powershell
./demo.ps1 ayuda
```

Si Windows no deja correr el script, se puede llamar así: `pwsh -ExecutionPolicy Bypass -File ./demo.ps1 ayuda`.

## Antes del viernes

- [ ] Docker Desktop instalado, con la integración con Ubuntu apagada (*Settings → Resources → WSL integration*). `docker version` muestra *Server: Docker Desktop*.
- [ ] `.env` creado (`Copy-Item .env.example .env` si no existe). El panel completa solo lo que falte de `.env.example`, sin tocar el `.env`.
- [ ] Un ensayo completo con esta guía, de principio a fin.
- [ ] Notificaciones andando en Render ([`render.yaml`](../render.yaml), [ADR-013](adr/ADR-013-capacidad-y-costos.md)) y probado `notificar` contra su URL (ver [Capacidad en la nube](#capacidad-en-la-nube)).
- [ ] Repasar el [diagrama interactivo del recorrido de un pedido](https://claude.ai/artifact/7ciK9Wg1i2kfvsxQW7FGCC), sobre todo la parte de "¿Y si se cae…?".

## El día de la presentación

Media hora antes:

```powershell
./demo.ps1 levantar     # unos minutos la primera vez (arma las imágenes); después, unos 15 s
./demo.ps1 cargar       # categorías, tres budines y cupo para la semana
./demo.ps1 abrir        # las direcciones para dejar abiertas en el navegador
```

Abrir la URL del gateway de Render para despertarlo: si estuvo 15 minutos sin tráfico, tarda en arrancar.

Pestañas para dejar abiertas:

| Qué | Dirección |
|---|---|
| Frontend | http://localhost:5173 |
| Grafana, tablero "Recién hecho" | http://localhost:3000 |
| Jaeger | http://localhost:16686 |
| Alertas de Prometheus | http://localhost:9090/alerts |
| RabbitMQ (usuario y clave en `.env`) | http://localhost:15672 |
| Mailpit | http://localhost:8025 |

El vendedor entra con `VENDEDOR_EMAIL` y `VENDEDOR_CLAVE` del `.env`.

## Recorrido por lo que pide el enunciado

| Punto | Qué mostrar | Cómo |
|---|---|---|
| Frontend completo | Registrarse, buscar un budín, encargarlo y verlo en "Mis pedidos". Después, como vendedor, avanzarlo de estado | En el frontend: `/registrarse`, `/`, `/mis-pedidos`, `/vendedor/pedidos` |
| Punto de entrada único | Autenticación, ruteo y límite de tráfico en el gateway | `./demo.ps1 pedir GET /categorias` (con sesión), `$env:SIN_SESION='1'; ./demo.ps1 pedir GET /pedidos` (`401`), `./demo.ps1 rafaga` (`429` después de 60 por minuto) |
| Tres servicios operativos | Los contenedores en marcha | `./demo.ps1 estado` |
| Capacidad propia en la nube | Notificaciones en Render, con URL pública | [Capacidad en la nube](#capacidad-en-la-nube) |
| Capacidad del proveedor | Pedidos le reserva insumos al inventario, detrás del disyuntor | Encargar un budín. Para mostrar fallas: `./demo.ps1 inventario lento` o `sin_stock`, y volver con `./demo.ps1 inventario confirma` |
| Dos tipos de almacenamiento | MySQL (productos, usuarios), MongoDB (pedidos, notificaciones), Redis (caché) y Meilisearch (búsqueda) | `./demo.ps1 estado`; [ADR-002](adr/ADR-002-persistencia.md) |
| Caché con medición | Prendida contra apagada | [La caché en vivo](#la-caché-en-vivo) |
| Dos estilos de arquitectura interna | Hexagonal en notificaciones, pedidos y productos; capas en usuarios | [ADR-007](adr/ADR-007-arquitectura-interna.md) y las carpetas `internal/` |
| Evento por mensajería | Cambiar el estado de un pedido como vendedor: pedidos publica el evento, notificaciones lo consume y llega el mail | Mailpit; `./demo.ps1 colas` muestra `notificaciones.pedidos` y la DLQ `notificaciones.fallidos` |
| Búsqueda indexada | Buscar, filtrar, ordenar y paginar en el catálogo; reconstruir el índice | Frontend; `./demo.ps1 reindexar` |
| Evidencia de las decisiones | Tablero, trazas, alertas y resultados de carga | Grafana, Jaeger, [ADR-013](adr/ADR-013-capacidad-y-costos.md#resultados), [POSTMORTEM.md](POSTMORTEM.md) |
| Limitaciones y deuda técnica | La sección de limitaciones | [ARCHITECTURE.md](ARCHITECTURE.md) |

### La caché en vivo

```powershell
./demo.ps1 cache apagar      # reinicia productos sin caché (unos segundos sin catálogo)
./demo.ps1 carga             # 30 s de carga sobre el catálogo
./demo.ps1 cache prender
./demo.ps1 carga
./demo.ps1 cache estado      # aciertos y fallos por tipo de clave
```

En Grafana, los paneles "Test de carga" muestran las dos corridas por separado: el `testid` dice si la caché estaba prendida o apagada. La medición completa (10 minutos por corrida) está en [ADR-009](adr/ADR-009-cache.md): el p95 del listado baja de 4,6 a 2,9 ms y MySQL deja de trabajar. Que la caché "apagada" no rompe nada también es evidencia: Redis no es obligatorio.

### Trazas

Después de encargar un budín, en Jaeger elegir el servicio `api-gateway` y abrir la última traza. Se ve el recorrido completo, incluido el salto por RabbitMQ hasta notificaciones.

## Si el profe pide…

| Pedido | Comando |
|---|---|
| Apagar o prender la caché | `./demo.ps1 cache apagar`, `./demo.ps1 cache prender` |
| Tirar una solicitud cualquiera | `./demo.ps1 pedir GET /productos`, `./demo.ps1 pedir POST /categorias '{"nombre":"Frutales"}'` |
| Buscar | `./demo.ps1 pedir GET "/productos/busqueda?q=limon&disponible=true"` |
| Mandar una notificación | `./demo.ps1 notificar cliente@ejemplo.com` (el mail aparece en Mailpit) |
| Probar la idempotencia | `$env:IDEMPOTENCIA='prueba-1'; ./demo.ps1 notificar a@ejemplo.com; ./demo.ps1 notificar a@ejemplo.com; ./demo.ps1 notificar otro@ejemplo.com; $env:IDEMPOTENCIA=$null`. La segunda devuelve la misma notificación; la tercera, `409` |
| Superar el límite de tráfico | `./demo.ps1 rafaga` |
| Tirar una instancia de notificaciones (balanceo) | `./demo.ps1 caer notificaciones-1`, mandar notificaciones y ver el panel "Instancias de notificaciones en el balanceo"; `./demo.ps1 volver notificaciones-1` |
| Tirar el servidor de correo | `./demo.ps1 caer mailpit`: las notificaciones quedan `pendiente` y salen al volver con `./demo.ps1 volver mailpit` |
| Tirar Redis | `./demo.ps1 caer redis-productos`: el catálogo sigue andando desde MySQL; `./demo.ps1 volver redis-productos` |
| Tirar RabbitMQ | `./demo.ps1 caer rabbitmq`: los pedidos se siguen tomando y los eventos esperan en la bandeja de salida; `./demo.ps1 volver rabbitmq` |
| Inventario lento, sin stock o con error | `./demo.ps1 inventario lento` (o `sin_stock`, `error`, `rechazo`); volver con `./demo.ps1 inventario confirma` |
| Ver qué servicios se pueden tirar | `./demo.ps1 servicios` |
| Ver los logs de un servicio | `docker compose logs --tail 50 pedidos` |

Qué pasa en cada caída, y por qué, está en [ADR-011](adr/ADR-011-resiliencia.md), en [POSTMORTEM.md](POSTMORTEM.md) y en el diagrama interactivo.

## Capacidad en la nube

Notificaciones corre en Render con el api-gateway adelante ([ADR-013](adr/ADR-013-capacidad-y-costos.md)). El contrato está en [`docs/contracts/notificaciones`](contracts/notificaciones/README.md), en el repo público. Para mostrarla:

```powershell
$env:GATEWAY = 'https://<gateway>.onrender.com'
$env:CLAVE_API = '<clave del otro grupo>'
./demo.ps1 notificar <un mail real>
$env:GATEWAY = $null; $env:CLAVE_API = $null
```

Si responde `503` la primera vez, es Render despertando: esperar unos segundos y repetir.

## Si algo falla

| Síntoma | Qué hacer |
|---|---|
| `docker` no se reconoce | Abrir Docker Desktop y esperar a que diga *Engine running* |
| `levantar` falla con "container name is already in use" | `./demo.ps1 detener` y volver a levantar |
| El frontend no llega al gateway | Hay otro programa usando el puerto 5173 (por ejemplo, un `bun run dev` olvidado): cerrarlo |
| Una solicitud da `503` | El servicio está caído o arrancando: `./demo.ps1 estado` |
| `notificar` queda en `pendiente` | Mailpit caído: `./demo.ps1 volver mailpit` |
| `cargar` falla al iniciar sesión | El gateway deja 10 intentos por minuto: esperar un minuto |

## Al terminar

```powershell
./demo.ps1 detener      # baja todo; los datos quedan para la próxima vez
```
