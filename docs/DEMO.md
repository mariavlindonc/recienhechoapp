# Guía de la presentación

Cómo levantar Recién hecho y mostrar cada punto del enunciado en una PC con Windows, PowerShell 7 y Docker Desktop, sin depender de nadie. Todos los comandos se corren **desde la raíz del repo**. Los resultados que figuran al lado de cada comando son los que dio el ensayo del 08/11/2026.

Hay dos tipos de comandos:

- **Comandos crudos** (`docker compose`, `Invoke-RestMethod`, `redis-cli`, `rabbitmqctl`): son la mayoría, se copian y se pueden cambiar en el momento.
- **Atajos de [`demo.ps1`](../demo.ps1)**: solo para lo que es largo de tipear (levantar, cargar datos, la prueba de concurrencia y los tests de integración). Antes de hacer cada paso, imprimen en gris lo que corren, así se ve qué pasa.

## Índice

- [Antes del viernes](#antes-del-viernes)
- [Arrancar](#arrancar)
- [Preparar la terminal](#preparar-la-terminal)
- [Qué mostrar por cada punto del enunciado](#qué-mostrar-por-cada-punto-del-enunciado)
- [Contenedores](#contenedores) · [Gateway](#gateway) · [Balanceo](#balanceo) · [Caché](#caché) · [Búsqueda](#búsqueda) · [Pedidos y consistencia](#pedidos-y-consistencia) · [Mensajería](#mensajería) · [Notificaciones](#notificaciones) · [Capacidad en la nube](#capacidad-en-la-nube) · [Proveedor de inventario](#proveedor-de-inventario) · [Caídas](#caídas) · [Observabilidad](#observabilidad) · [Ver los datos en las bases](#ver-los-datos-en-las-bases) · [Carga](#carga) · [Tests](#tests)
- [Lo que todavía no está](#lo-que-todavía-no-está)
- [Si algo falla](#si-algo-falla) · [Al terminar](#al-terminar)

## Antes del viernes

- [ ] Todo mergeado a `main` y la PC en `main` con `git pull`. El viernes corre lo que haya ahí.
- [ ] Docker Desktop instalado, con la integración con Ubuntu apagada (*Settings → Resources → WSL integration*). `docker version` tiene que mostrar *Server: Docker Desktop*.
- [ ] Un ensayo completo con esta guía, de principio a fin, en la PC de la presentación.
- [ ] Notificaciones andando en Render y probado el `POST` con la clave del otro grupo ([Capacidad en la nube](#capacidad-en-la-nube)).

## Arrancar

Media hora antes. Abrir Docker Desktop y esperar a que diga *Engine running*. Después:

```powershell
./demo.ps1 levantar     # completa .env, levanta todo y espera al gateway y al frontend. 40 s con las imágenes hechas; unos minutos la primera vez
./demo.ps1 cargar       # tres categorías, tres budines y cupo de 10 para hoy y los próximos 7 días
```

`levantar` crea `.env` desde `.env.example` si no existe, y si le faltan variables las agrega al final con el valor de ejemplo. Lo que ya está en `.env` no se toca. Por eso, después, `docker compose` a secas funciona igual que el atajo.

Si Windows no deja correr el script: `pwsh -ExecutionPolicy Bypass -File ./demo.ps1 levantar`.

Abrir las pestañas:

```powershell
'http://localhost:5173', 'http://localhost:3000', 'http://localhost:16686', 'http://localhost:9090/alerts', 'http://localhost:15672', 'http://localhost:8025' | ForEach-Object { Start-Process $_ }
```

| Pestaña | Dirección | Entrar con |
|---|---|---|
| Frontend | http://localhost:5173 | `vendedor@recienhecho.test` / `vendedor-desarrollo`, o registrarse como comprador |
| Grafana, tablero "Recién hecho" | http://localhost:3000 | Sin usuario (solo lectura) |
| Jaeger | http://localhost:16686 | — |
| Alertas de Prometheus | http://localhost:9090/alerts | — |
| RabbitMQ | http://localhost:15672 | `recienhecho` / `desarrollo` |
| Mailpit (los mails) | http://localhost:8025 | — |
| Meilisearch | http://localhost:7700 | Clave `una-clave-maestra-de-desarrollo` |

Abrir el gateway de Render para despertarlo: `Invoke-RestMethod https://recienhecho-api-gateway.onrender.com/health` (la primera vez tarda unos 15 s).

## Preparar la terminal

Pegar este bloque una vez en cada terminal nueva. Deja variables cortas para los comandos de abajo: `$gw` (el gateway), `$vendedor` y `$comprador` (sus sesiones), `$clave` (la clave de API de notificaciones), y los accesos a Prometheus, RabbitMQ y Meilisearch. Los usuarios y claves son los de `.env.example`; si los cambiaste en `.env`, cambialos acá.

```powershell
[Console]::OutputEncoding = [Text.Encoding]::UTF8   # para que las tildes de Docker se vean bien
$gw = 'http://localhost:8080'
$prom = 'http://localhost:9090/api/v1/query'
$json = 'application/json; charset=utf-8'
$e = Get-Content .env | Where-Object { $_ -match '^[A-Z_]+=' } | ConvertFrom-StringData   # las variables de .env, como $e.NOMBRE
$vendedor = @{ Authorization = 'Bearer ' + (Invoke-RestMethod -Method Post "$gw/usuarios/sesion" -ContentType $json -Body '{"email":"vendedor@recienhecho.test","clave":"vendedor-desarrollo"}').token }
Invoke-WebRequest -Method Post "$gw/usuarios" -ContentType $json -Body '{"email":"comprador@ejemplo.com","nombre":"Comprador","clave":"comprador-demo"}' -SkipHttpErrorCheck | Out-Null
$comprador = @{ Authorization = 'Bearer ' + (Invoke-RestMethod -Method Post "$gw/usuarios/sesion" -ContentType $json -Body '{"email":"comprador@ejemplo.com","clave":"comprador-demo"}').token }
$clave = @{ 'X-API-Key' = 'una-clave-larga-de-desarrollo' }
$rabbit = @{ Authorization = 'Basic ' + [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes('recienhecho:desarrollo')) }
$meili = @{ Authorization = 'Bearer una-clave-maestra-de-desarrollo' }
```

Las sesiones vencen a la hora: si algo empieza a dar `401`, volver a pegar el bloque. El gateway deja 10 inicios de sesión por minuto por IP; si da `429`, esperar un minuto.

Dos formas de pedir, para todo lo que sigue:

- `Invoke-RestMethod` devuelve el cuerpo ya convertido en objeto. Si la respuesta es un error (4xx o 5xx), corta con una excepción roja.
- `Invoke-WebRequest ... -SkipHttpErrorCheck` no corta nunca y deja ver el código: `.StatusCode` y `.Content`. Se usa para mostrar errores a propósito.

## Qué mostrar por cada punto del enunciado

| Punto de la presentación | Dónde está en esta guía |
|---|---|
| Frontend completo, del ingreso a la funcionalidad compartida | En el navegador: `/registrarse`, catálogo, detalle, encargar, `/mis-pedidos`; como vendedor, `/vendedor/pedidos` para avanzar estados (llega un mail en cada uno) |
| Punto de entrada único con autenticación, ruteo y control de tráfico | [Gateway](#gateway) |
| Al menos tres servicios operativos | [Contenedores](#contenedores) |
| Capacidad propia en la nube con URL pública | [Capacidad en la nube](#capacidad-en-la-nube) |
| Capacidad del proveedor en el flujo principal | [Proveedor de inventario](#proveedor-de-inventario) (hoy con el simulador: ver [Lo que todavía no está](#lo-que-todavía-no-está)) |
| Dos tipos de almacenamiento | [Ver los datos en las bases](#ver-los-datos-en-las-bases): MySQL (productos, usuarios) y MongoDB (pedidos, notificaciones), más Redis y Meilisearch |
| Caché con medición antes y después | [Caché](#caché) y [Carga](#carga) |
| Dos estilos de arquitectura interna | Hexagonal en notificaciones, pedidos y productos (`internal/dominio`, `aplicacion`, `puertos`, `adaptadores`); capas en usuarios (`handler`, `servicio`, `modelo`). [ADR-007](adr/ADR-007-arquitectura-interna.md) |
| Evento por mensajería, consumidor idempotente y mensajes no procesables | [Mensajería](#mensajería) |
| Búsqueda con paginación, filtros, orden y reconstrucción del índice | [Búsqueda](#búsqueda) |
| Verificación de las decisiones con evidencia | [Observabilidad](#observabilidad), [Carga](#carga), [ADR-013](adr/ADR-013-capacidad-y-costos.md#resultados), [POSTMORTEM.md](POSTMORTEM.md) |
| Limitaciones y deuda técnica | [ARCHITECTURE.md](ARCHITECTURE.md), sección de limitaciones |

Para la defensa individual, además: [Pedidos y consistencia](#pedidos-y-consistencia) (operación crítica), [Tests](#tests), [Balanceo](#balanceo), [Caídas](#caídas) y [Carga](#carga).

## Contenedores

```powershell
docker compose ps -a                                  # todos los contenedores, con su estado y si están sanos (healthy)
docker compose config --services                      # los nombres que aceptan los comandos de abajo
docker compose logs --tail 30 pedidos                 # las últimas 30 líneas de un servicio
docker compose logs -f api-gateway                    # seguir los logs en vivo (Ctrl+C para salir)
docker compose restart productos                      # reiniciar un servicio
docker compose up -d --build pedidos                  # reconstruir y levantar uno solo, después de cambiar su código
docker stats --no-stream --format '{{.Name}} {{.MemUsage}} {{.CPUPerc}}'   # memoria y CPU de cada contenedor
Invoke-RestMethod "$gw/health"                        # el gateway responde: {"service":"api-gateway","status":"ok"}
```

Los servicios no publican puerto: se entra solo por el gateway (`localhost:8080`). Las herramientas (bases, Grafana, Prometheus...) publican su puerto solo en `127.0.0.1`.

## Gateway

Todo entra por `localhost:8080` ([ADR-001](adr/ADR-001-limites-de-los-servicios.md), [ADR-016](adr/ADR-016-autenticacion.md)). El ruteo es por prefijo:

| Prefijo | Va a |
|---|---|
| `/usuarios` | usuarios |
| `/productos`, `/categorias`, `/imagenes` | productos |
| `/pedidos`, `/capacidad` | pedidos |
| `/notificaciones` | notificaciones (dos instancias, con balanceo) |
| `/internos/...` | no se rutea: solo lo usan los servicios entre ellos |

**Ruteo:**

```powershell
Invoke-RestMethod "$gw/categorias"                                              # productos responde por el gateway
(Invoke-RestMethod "$gw/usuarios/yo" -Headers $vendedor) | Format-List          # usuarios: el perfil del vendedor
(Invoke-WebRequest "$gw/internos/productos/x" -SkipHttpErrorCheck).StatusCode   # 404: lo interno no sale por el gateway
(Invoke-WebRequest "$gw/no-existe" -SkipHttpErrorCheck).StatusCode              # 404
```

**Autenticación y roles.** El gateway valida el token (JWT) y le pasa al servicio quién es el usuario en los encabezados `X-Id-Usuario`, `X-Email-Usuario` y `X-Rol-Usuario`. Si el cliente manda esos encabezados, el gateway los borra y los arma con el token:

```powershell
(Invoke-WebRequest "$gw/pedidos" -SkipHttpErrorCheck).StatusCode                                                  # 401: sin token
(Invoke-WebRequest "$gw/pedidos" -Headers @{ Authorization = 'Bearer inventado' } -SkipHttpErrorCheck).StatusCode # 401: token falso
(Invoke-WebRequest "$gw/pedidos/especiales" -Headers $comprador -SkipHttpErrorCheck).StatusCode                    # 403: un comprador en una ruta de vendedor
(Invoke-WebRequest "$gw/pedidos/especiales" -Headers ($comprador + @{ 'X-Rol-Usuario' = 'vendedor' }) -SkipHttpErrorCheck).StatusCode   # 403: aunque diga que es vendedor
(Invoke-WebRequest "$gw/pedidos/especiales" -Headers $vendedor -SkipHttpErrorCheck).StatusCode                     # 200: el vendedor sí entra
(Invoke-WebRequest "$gw/notificaciones/x" -SkipHttpErrorCheck).StatusCode                                         # 401: notificaciones pide X-API-Key
```

**Control de tráfico.** Dos límites ([ADR-004](adr/ADR-004-contrato-de-notificaciones.md), [ADR-019](adr/ADR-019-sesion-en-cookie.md)):

```powershell
# 60 notificaciones por minuto por grupo: con 70 seguidas, 60 dan 202 y 10 dan 429. Manda 60 mails a Mailpit.
1..70 | ForEach-Object { (Invoke-WebRequest -Method Post "$gw/notificaciones" -Headers ($clave + @{ 'Idempotency-Key' = [guid]::NewGuid().ToString() }) -ContentType $json -Body '{"canal":"email","destinatario":"rafaga@ejemplo.com","asunto":"Ráfaga","cuerpo":"Prueba del límite"}' -SkipHttpErrorCheck).StatusCode } | Group-Object | Select-Object Count, Name

# Cuánto esperar: el 429 trae Retry-After (dio 60)
(Invoke-WebRequest -Method Post "$gw/notificaciones" -Headers ($clave + @{ 'Idempotency-Key' = [guid]::NewGuid().ToString() }) -ContentType $json -Body '{"canal":"email","destinatario":"a@ejemplo.com","asunto":"x","cuerpo":"x"}' -SkipHttpErrorCheck).Headers['Retry-After']

# 10 inicios de sesión por minuto por IP, contando los que salen bien: pasado eso, 429. Después hay que esperar un minuto para volver a entrar.
1..12 | ForEach-Object { (Invoke-WebRequest -Method Post "$gw/usuarios/sesion" -ContentType $json -Body '{"email":"intruso@ejemplo.com","clave":"probando-claves"}' -SkipHttpErrorCheck).StatusCode } | Group-Object | Select-Object Count, Name
```

Para probar el límite con otro valor: `$env:NOTIFICACIONES_LIMITE_POR_MINUTO = '5'; docker compose up -d api-gateway; $env:NOTIFICACIONES_LIMITE_POR_MINUTO = $null`. Un `docker compose up -d api-gateway` sin la variable vuelve al valor de `.env`.

## Balanceo

El gateway reparte `/notificaciones` entre `notificaciones-1` y `notificaciones-2`, una y una. Cada 5 s les pide `/vivo`: una instancia sale del balanceo con dos chequeos fallidos seguidos o con un reenvío fallido, y vuelve con un chequeo OK ([ADR-012](adr/ADR-012-balanceo-de-carga.md)). En Grafana se ve en los paneles "Reparto del tráfico entre instancias" e "Instancias de notificaciones en el balanceo".

```powershell
# Cuántas solicitudes recibió cada instancia (Prometheus lee cada 15 s: esperar 15 s después de mandar)
(Invoke-RestMethod $prom -Body @{ query = 'sum by (instancia) (gateway_solicitudes_total{servicio="notificaciones"})' }).data.result | ForEach-Object { "$($_.metric.instancia): $($_.value[1])" }

# 20 consultas por el gateway (dan 404 porque el id no existe, pero llegan a notificaciones): el reparto queda parejo (dio 11 y 10)
1..20 | ForEach-Object { Invoke-WebRequest "$gw/notificaciones/no-existe-$_" -Headers $clave -SkipHttpErrorCheck | Out-Null }

# Qué instancias están en el balanceo: 1 adentro, 0 afuera
(Invoke-RestMethod $prom -Body @{ query = 'gateway_instancia_sana{servicio="notificaciones"}' }).data.result | ForEach-Object { "$($_.metric.instancia): $($_.value[1])" }
```

**Tirar una instancia:**

```powershell
docker compose stop notificaciones-1
1..10 | ForEach-Object { (Invoke-WebRequest "$gw/notificaciones/x-$_" -Headers $clave -SkipHttpErrorCheck).StatusCode } | Group-Object | Select-Object Count, Name
docker compose start notificaciones-1      # vuelve sola al balanceo en el chequeo siguiente (unos 5 s)
```

Resultado: **la primera solicitud que le tocaba a la instancia caída da `503`**. Con ese reenvío fallido el gateway la saca, y las 9 siguientes van a la otra. El gateway no reintenta solo: el cliente reintenta con la misma `Idempotency-Key`, como pide el contrato.

**Colgar una instancia** (sigue prendida pero no responde, como un servidor lento):

```powershell
docker compose pause notificaciones-2
1..6 | ForEach-Object { Measure-Command { $script:codigo = (Invoke-WebRequest "$gw/notificaciones/y-$_" -Headers $clave -SkipHttpErrorCheck).StatusCode } | ForEach-Object { "$script:codigo en $([math]::Round($_.TotalSeconds, 1)) s" } }
docker compose unpause notificaciones-2
```

Resultado: la primera espera los **3 s** del timeout del gateway y da `503`. Las siguientes salen al instante por la otra instancia.

## Caché

Redis guarda el catálogo, los productos y las categorías por **60 s** ([ADR-009](adr/ADR-009-cache.md)). Cuando cambia un producto, se borra su clave y sube `catalogo:version`, así que las páginas viejas del catálogo dejan de usarse.

```powershell
docker compose exec redis-productos redis-cli --scan                     # todas las claves que hay ahora
docker compose exec redis-productos redis-cli get catalogo:version       # la versión del catálogo
$producto = (Invoke-RestMethod "$gw/productos").productos | Select-Object -First 1
Invoke-RestMethod "$gw/productos/$($producto.id)" | Out-Null              # la primera vez va a MySQL y lo guarda
docker compose exec redis-productos redis-cli ttl "producto:$($producto.id)"   # segundos que le quedan (60 o menos)
docker compose exec redis-productos redis-cli flushall                  # vaciar la caché entera
```

**Invalidación:** cambiar el precio y ver que se borra la clave y sube la versión.

```powershell
$cambio = @{ nombre = $producto.nombre; descripcion = $producto.descripcion; categoriaId = $producto.categoriaId; precioCentavos = $producto.precioCentavos + 100; anticipacionMinimaDias = $producto.anticipacionMinimaDias; aptoCeliacos = $producto.aptoCeliacos; disponible = $producto.disponible } | ConvertTo-Json
Invoke-RestMethod -Method Put "$gw/productos/$($producto.id)" -Headers $vendedor -ContentType $json -Body $cambio | Select-Object nombre, precioCentavos
docker compose exec redis-productos redis-cli exists "producto:$($producto.id)"   # 0: se borró
docker compose exec redis-productos redis-cli get catalogo:version             # subió
```

**Aciertos y fallos** (desde que arrancó productos):

```powershell
foreach ($metrica in 'cache_aciertos_total', 'cache_fallos_total') { "${metrica}: " + (((Invoke-RestMethod $prom -Body @{ query = "sum by (tipo) ($metrica)" }).data.result | ForEach-Object { "$($_.metric.tipo)=$($_.value[1])" }) -join ' ') }
(Invoke-RestMethod $prom -Body @{ query = 'sum(cache_errores_total)' }).data.result.value[1]   # veces que Redis falló o tardó más de 50 ms y se siguió sin caché
```

**Apagar y prender la caché** (reinicia productos, unos segundos sin catálogo):

```powershell
$env:CACHE_HABILITADA = 'false'; docker compose up -d productos; $env:CACHE_HABILITADA = $null   # apagada
docker compose logs --no-log-prefix productos | Select-String '"cache":(true|false)' | Select-Object -Last 1   # confirma con qué arrancó
docker compose up -d productos                                                                   # prendida otra vez (el valor de .env)
```

La comparación con números va en [Carga](#carga). La medición completa de 10 minutos está en [ADR-009](adr/ADR-009-cache.md): el p95 del listado bajó de 4,6 a 2,9 ms.

**Redis caído:** `docker compose stop redis-productos`. El catálogo sigue respondiendo desde MySQL (dio `200` en 58 ms) y sube `cache_errores_total`. Volver con `docker compose start redis-productos`.

## Búsqueda

Meilisearch indexa los productos ([ADR-008](adr/ADR-008-busqueda.md)). Parámetros de `GET /productos/busqueda`:

| Parámetro | Qué hace |
|---|---|
| `q` | texto (tolera errores de tipeo) |
| `categoria` | id de categoría |
| `precioMin`, `precioMax` | en **centavos** (12312300 son $123.123) |
| `aptoCeliacos`, `disponible` | `true` o `false` |
| `orden` | vacío (relevancia), `precio_asc`, `precio_desc` o `novedad` |
| `pagina` | desde 1; 20 por página |

```powershell
$r = Invoke-RestMethod "$gw/productos/busqueda" -Body @{ q = 'zanahoria'; orden = 'precio_desc'; pagina = 1 }
"total $($r.total), página $($r.pagina) de a $($r.porPagina)"; $r.productos | Format-Table nombre, precioCentavos, aptoCeliacos, disponible
(Invoke-RestMethod "$gw/productos/busqueda" -Body @{ aptoCeliacos = 'true'; disponible = 'true' }).productos | Format-Table nombre, precioCentavos
(Invoke-RestMethod "$gw/productos/busqueda" -Body @{ precioMax = 50000000; orden = 'precio_asc' }).productos | Format-Table nombre, precioCentavos
(Invoke-RestMethod "$gw/productos/busqueda" -Body @{ q = 'zanaoria' }).productos.nombre        # con error de tipeo igual encuentra
```

**Retraso tolerable (RNF1, menos de 10 s).** Cambiar un precio con el bloque de [Caché](#caché) y medir cuánto tarda en verse en la búsqueda. En el ensayo tardó 1,4 s:

```powershell
$nuevo = $producto.precioCentavos + 200
$cambio = @{ nombre = $producto.nombre; descripcion = $producto.descripcion; categoriaId = $producto.categoriaId; precioCentavos = $nuevo; anticipacionMinimaDias = $producto.anticipacionMinimaDias; aptoCeliacos = $producto.aptoCeliacos; disponible = $producto.disponible } | ConvertTo-Json
$reloj = [Diagnostics.Stopwatch]::StartNew(); Invoke-RestMethod -Method Put "$gw/productos/$($producto.id)" -Headers $vendedor -ContentType $json -Body $cambio | Out-Null
do { Start-Sleep -Milliseconds 250; $visto = ((Invoke-RestMethod "$gw/productos/busqueda" -Body @{ q = $producto.nombre }).productos | Where-Object id -EQ $producto.id).precioCentavos } until ($visto -eq $nuevo -or $reloj.Elapsed.TotalSeconds -gt 20)
"la búsqueda lo vio a los $([math]::Round($reloj.Elapsed.TotalSeconds, 1)) s"
(Invoke-RestMethod $prom -Body @{ query = 'max(indice_pendiente_antiguedad_segundos)' }).data.result.value[1]   # cuánto espera el cambio más viejo sin reflejarse (0 si no hay)
```

**Reconstruir el índice** desde MySQL:

```powershell
docker compose exec productos /servicio reindexar        # "índice de búsqueda reconstruido", con la cantidad de productos
(Invoke-RestMethod http://localhost:7700/indexes/productos/stats -Headers $meili) | Format-List   # Meilisearch directo: documentos en el índice
```

**Meilisearch caído:** `docker compose stop meilisearch`. La búsqueda da `503` y el catálogo sigue andando (`200`). Los cambios de productos esperan en la tabla `indice_pendiente` y se aplican solos al volver; si pasan 10 s se dispara la alerta `BusquedaAtrasada`. Volver con `docker compose start meilisearch`.

## Pedidos y consistencia

La operación crítica es **reservar cupo**: un día tiene una cantidad máxima de pedidos y nunca se vende de más, aunque lleguen muchos a la vez. El pedido, el cupo y el evento se guardan en una sola transacción de MongoDB ([ADR-015](adr/ADR-015-consistencia-y-concurrencia.md)).

**La prueba de concurrencia** (atajo): pone cupo 3 en un día libre y manda 10 pedidos al mismo tiempo.

```powershell
./demo.ps1 concurrencia 10 3     # resultado: 3 x 201 aceptados, 7 x 409 sin cupo, "OK: no se vendió de más"
./demo.ps1 concurrencia 50 5     # con más carga
```

**Encargar y avanzar un pedido** a mano:

```powershell
$producto = (Invoke-RestMethod "$gw/productos").productos | Select-Object -First 1
$fecha = (Get-Date).AddDays(2).ToString('yyyy-MM-dd')
$cuerpo = @{ tipo = 'programado'; fecha = $fecha; items = @(@{ productoId = $producto.id; cantidad = 2 }) } | ConvertTo-Json -Depth 5
$llave = [guid]::NewGuid().ToString()
$pedido = Invoke-RestMethod -Method Post "$gw/pedidos" -Headers ($comprador + @{ 'Idempotency-Key' = $llave }) -ContentType $json -Body $cuerpo
$pedido | Select-Object id, estado, fecha, precioCentavos
(Invoke-RestMethod -Method Post "$gw/pedidos" -Headers ($comprador + @{ 'Idempotency-Key' = $llave }) -ContentType $json -Body $cuerpo).id -eq $pedido.id   # True: repetirlo con la misma clave devuelve el mismo pedido
```

`tipo` puede ser `programado` (con `fecha`) o `inmediato` (sin fecha: toma el primer día con cupo). El pedido pasa solo de `pendiente_confirmacion` a `confirmado` cuando el inventario reserva los insumos.

```powershell
Invoke-RestMethod "$gw/pedidos/$($pedido.id)" -Headers $comprador | Select-Object id, estado
Invoke-RestMethod -Method Put "$gw/pedidos/$($pedido.id)/estado" -Headers $vendedor -ContentType $json -Body '{"estado":"en_produccion"}' | Select-Object estado
$salto = Invoke-WebRequest -Method Put "$gw/pedidos/$($pedido.id)/estado" -Headers $vendedor -ContentType $json -Body '{"estado":"entregado"}' -SkipHttpErrorCheck
"$($salto.StatusCode) $($salto.Content)"                       # 409 transicion_invalida: no se puede saltar estados
Invoke-RestMethod "$gw/pedidos" -Headers $vendedor -Body @{ fecha = $fecha } | Format-Table id, estado, fecha, precioCentavos   # el vendedor los ve por día (sin fecha da 400)
Invoke-RestMethod "$gw/pedidos" -Headers $comprador | Format-Table id, estado, fecha                                           # el comprador ve todos los suyos
```

Estados: `pendiente_confirmacion` → `confirmado` → `en_produccion` → `listo` → `en_camino` (opcional) → `entregado`. Se puede cancelar en `pendiente_confirmacion` y `confirmado`. Cada cambio manda un mail al comprador.

**Cupo:**

```powershell
Invoke-RestMethod -Method Put "$gw/capacidad/$fecha" -Headers $vendedor -ContentType $json -Body '{"capacidad":5}'
Invoke-RestMethod "$gw/capacidad" -Headers $vendedor -Body @{ desde = $fecha; hasta = $fecha } | ConvertTo-Json   # capacidad y reservados de ese día
```

Bajar el cupo por debajo de lo ya reservado da `409 capacidad_menor_que_reservados`.

## Mensajería

Pedidos publica `pedido.estado_cambiado` en el exchange `pedidos` de RabbitMQ. Notificaciones lo consume de la cola `notificaciones.pedidos` y manda el mail ([ADR-003](adr/ADR-003-comunicacion-entre-servicios.md)). El evento se guarda primero en `eventos_pendientes`, en la misma transacción que el pedido (bandeja de salida), y se publica después: si RabbitMQ está caído, el pedido se toma igual.

```powershell
docker compose exec rabbitmq rabbitmqctl list_queues name messages consumers   # notificaciones.pedidos (2 consumidores) y la de fallidos
(Invoke-RestMethod $prom -Body @{ query = 'sum by (resultado) (pedidos_eventos_publicados_total)' }).data.result | ForEach-Object { "$($_.metric.resultado): $($_.value[1])" }
```

**Consumidor idempotente.** Publicar el mismo evento dos veces, a mano, con el mismo `idEvento`: tiene que llegar **un solo mail**.

```powershell
$evento = @{ idEvento = 'demo-' + (Get-Date -Format HHmmss); tipo = 'pedido.estado_cambiado'; version = 1; datos = @{ idPedido = 'demo'; emailComprador = 'duplicado@ejemplo.com'; estadoNuevo = 'listo'; fecha = '2026-11-13'; items = @(@{ nombreProducto = 'Budín de limón'; cantidad = 1 }) } } | ConvertTo-Json -Compress -Depth 5
$mensaje = @{ properties = @{}; routing_key = 'pedido.estado_cambiado'; payload = $evento; payload_encoding = 'string' } | ConvertTo-Json -Compress
1..2 | ForEach-Object { (Invoke-RestMethod -Method Post 'http://localhost:15672/api/exchanges/%2F/pedidos/publish' -Headers $rabbit -ContentType $json -Body $mensaje).routed }   # True, True
Start-Sleep 8; (Invoke-RestMethod 'http://localhost:8025/api/v1/search' -Body @{ query = 'to:duplicado@ejemplo.com' }).messages_count   # 1
```

**Mensaje no procesable.** Uno que no es JSON va directo a `notificaciones.fallidos`, sin reintentos. Uno que falla por algo pasajero se reintenta hasta el límite de entregas y después también va ahí:

```powershell
$roto = @{ properties = @{}; routing_key = 'pedido.estado_cambiado'; payload = 'esto no es JSON'; payload_encoding = 'string' } | ConvertTo-Json -Compress
Invoke-RestMethod -Method Post 'http://localhost:15672/api/exchanges/%2F/pedidos/publish' -Headers $rabbit -ContentType $json -Body $roto
docker compose exec rabbitmq rabbitmqctl list_queues name messages            # notificaciones.fallidos sube en 1
(Invoke-RestMethod -Method Post 'http://localhost:15672/api/queues/%2F/notificaciones.fallidos/get' -Headers $rabbit -ContentType $json -Body '{"count":5,"ackmode":"ack_requeue_true","encoding":"auto"}').payload   # mirarlos sin sacarlos
docker compose logs --no-log-prefix notificaciones-1 notificaciones-2 | Select-String 'no procesable' | Select-Object -Last 2
```

**RabbitMQ caído:** `docker compose stop rabbitmq`. Un pedido nuevo da `201` igual. `pedidos_eventos_publicados_total{resultado="error"}` sube, y el evento queda en la bandeja de salida. Con `docker compose start rabbitmq` se publica solo, en unos 20 s, y llega el mail.

## Notificaciones

Es la capacidad que publicamos para el otro grupo. El contrato está en [`docs/contracts/notificaciones`](contracts/notificaciones/README.md).

```powershell
$llave = [guid]::NewGuid().ToString()
$nueva = Invoke-RestMethod -Method Post "$gw/notificaciones" -Headers ($clave + @{ 'Idempotency-Key' = $llave }) -ContentType $json -Body '{"canal":"email","destinatario":"cliente@ejemplo.com","asunto":"Tu pedido está listo","cuerpo":"Ya podés retirarlo."}'
$nueva                                                                             # 202, estado pendiente
Start-Sleep 3; Invoke-RestMethod "$gw/notificaciones/$($nueva.id)" -Headers $clave   # estado enviada
(Invoke-RestMethod 'http://localhost:8025/api/v1/messages?limit=5').messages | ForEach-Object { "$($_.To[0].Address) | $($_.Subject)" }   # los últimos mails
```

**Idempotencia:**

```powershell
(Invoke-RestMethod -Method Post "$gw/notificaciones" -Headers ($clave + @{ 'Idempotency-Key' = $llave }) -ContentType $json -Body '{"canal":"email","destinatario":"cliente@ejemplo.com","asunto":"Tu pedido está listo","cuerpo":"Ya podés retirarlo."}').id -eq $nueva.id   # True: misma clave y mismo cuerpo, misma notificación
$otra = Invoke-WebRequest -Method Post "$gw/notificaciones" -Headers ($clave + @{ 'Idempotency-Key' = $llave }) -ContentType $json -Body '{"canal":"email","destinatario":"otro@ejemplo.com","asunto":"x","cuerpo":"x"}' -SkipHttpErrorCheck
"$($otra.StatusCode) $($otra.Content)"                                            # 409 conflicto_idempotencia: misma clave, otro cuerpo
$mal = Invoke-WebRequest -Method Post "$gw/notificaciones" -Headers ($clave + @{ 'Idempotency-Key' = [guid]::NewGuid().ToString() }) -ContentType $json -Body '{"canal":"sms","destinatario":"no-es-mail","asunto":"","cuerpo":"x"}' -SkipHttpErrorCheck
"$($mal.StatusCode) $($mal.Content)"                                              # 400 con todos los problemas juntos
```

**Servidor de correo caído:** `docker compose stop mailpit`, mandar unas notificaciones: el `POST` sigue dando `202` y quedan `pendiente`. Con 5 fallos seguidos se abre el disyuntor y se deja de intentar; cada ~35 s pasa un envío de prueba. `docker compose start mailpit` y salen. El log lo muestra:

```powershell
docker compose logs --no-log-prefix notificaciones-1 notificaciones-2 | Select-String 'disyuntor' | Select-Object -Last 5
```

**Mock del contrato** (para el otro grupo, sin depender del servicio real; responde siempre el ejemplo del contrato): `Invoke-RestMethod -Method Post http://localhost:4010/notificaciones -Headers @{ 'X-API-Key' = 'prueba'; 'Idempotency-Key' = 'x' } -ContentType $json -Body '{"canal":"email","destinatario":"a@ejemplo.com","asunto":"x","cuerpo":"x"}'`.

## Capacidad en la nube

Notificaciones corre en Render (plan gratis) con el gateway adelante ([ADR-013](adr/ADR-013-capacidad-y-costos.md)): https://recienhecho-api-gateway.onrender.com. Se duerme a los 15 minutos sin uso: el primer pedido tarda unos 15 s o da `503`.

```powershell
Invoke-RestMethod https://recienhecho-api-gateway.onrender.com/health                 # despertarlo
Invoke-RestMethod https://recienhecho-notificaciones.onrender.com/health               # notificaciones directo
(Invoke-WebRequest https://recienhecho-api-gateway.onrender.com/notificaciones/x -SkipHttpErrorCheck).StatusCode   # 401 sin clave (si da 503, esperar unos segundos: está despertando)

$render = @{ 'X-API-Key' = '<la clave del otro grupo, cargada en Render>' }
$enviada = Invoke-RestMethod -Method Post https://recienhecho-api-gateway.onrender.com/notificaciones -Headers ($render + @{ 'Idempotency-Key' = [guid]::NewGuid().ToString() }) -ContentType $json -Body '{"canal":"email","destinatario":"<un mail real>","asunto":"Prueba desde la presentación","cuerpo":"Hola"}'
Start-Sleep 5; Invoke-RestMethod "https://recienhecho-api-gateway.onrender.com/notificaciones/$($enviada.id)" -Headers $render
```

El mock público del contrato: https://recienhecho-notificaciones-mock.onrender.com (acepta cualquier `X-API-Key`).

## Proveedor de inventario

Pedidos le pide los insumos al inventario del otro grupo detrás de un puerto propio (capa anticorrupción) y de un disyuntor ([ADR-010](adr/ADR-010-consumo-de-inventario.md)). Hoy corre con **el simulador**, que se configura con `INVENTARIO_MODO`:

| Modo | Qué hace |
|---|---|
| `confirma` | reserva bien (el normal) |
| `sin_stock` | rechaza: el pedido queda `cancelado` con "No hay insumos para esa fecha." |
| `lento` | tarda más que el timeout de 1,5 s: el pedido queda `pendiente_confirmacion` y se reintenta |
| `error` | responde error: igual que lento, se reintenta |
| `rechazo` | rechaza por formato: se frena, con la alerta `InventarioRechazaReservas` |

```powershell
$env:INVENTARIO_MODO = 'sin_stock'; docker compose up -d pedidos; $env:INVENTARIO_MODO = $null   # cambia el modo (reinicia pedidos)
# encargar un pedido como en "Pedidos y consistencia" y mirar su estado a los 3 s
docker compose up -d pedidos                                                                     # vuelve a confirma (el valor de .env)
(Invoke-RestMethod $prom -Body @{ query = 'sum by (resultado) (pedidos_inventario_llamadas_total)' }).data.result | ForEach-Object { "$($_.metric.resultado): $($_.value[1])" }
(Invoke-RestMethod $prom -Body @{ query = 'pedidos_circuito_inventario == 1' }).data.result.metric.estado   # estado del disyuntor
```

El servicio real del otro grupo (https://stock-volcamelli-inventario.onrender.com) daba `404 no-server` el 06/11 y el 08/11. Como respaldo, por si no está el día de la demo, el compose tiene un **mock de su contrato** (`inventario-mock`, [ADR-020](adr/ADR-020-reserva-y-consumo-de-insumos.md#traducción-al-proveedor)). Responde siempre el ejemplo del contrato:

```powershell
docker compose up -d inventario-mock
Invoke-RestMethod http://localhost:4011/api/branches/1/products/1/stock -Headers @{ 'X-API-Key' = 'prueba' }
(Invoke-WebRequest -Method Post http://localhost:4011/api/branches/1/products/1/stock/decrease -Headers @{ 'X-API-Key' = 'prueba'; 'Prefer' = 'code=409' } -ContentType $json -Body '{"quantity":2,"reason":"prueba"}' -SkipHttpErrorCheck).Content   # fuerza el 409 de stock insuficiente
```

Pedidos todavía no le habla ni al mock ni al servicio real: ver [Lo que todavía no está](#lo-que-todavía-no-está).

## Caídas

Qué pasa cuando se cae cada dependencia ([ADR-011](adr/ADR-011-resiliencia.md), [POSTMORTEM.md](POSTMORTEM.md)). `stop` la apaga; `pause` la deja colgada (prendida pero sin responder), que sirve para mostrar timeouts. Siempre se vuelve con `start` o `unpause`.

| Dependencia | Para tirarla | Qué pasa (ensayo del 08/11) |
|---|---|---|
| Una instancia de notificaciones | `docker compose stop notificaciones-1` | La primera solicitud que le toca da `503`; las siguientes van a la otra. Vuelve sola al prenderla |
| Una instancia colgada | `docker compose pause notificaciones-2` | La primera espera 3 s y da `503`; después sale del balanceo |
| Redis | `docker compose stop redis-productos` | El catálogo sigue desde MySQL; sube `cache_errores_total` |
| Meilisearch | `docker compose stop meilisearch` | La búsqueda da `503`; el catálogo anda; los cambios esperan y se aplican al volver |
| RabbitMQ | `docker compose stop rabbitmq` | Los pedidos se toman igual; los eventos esperan en la bandeja de salida y se publican al volver |
| Mailpit (servidor de correo) | `docker compose stop mailpit` | El `POST /notificaciones` sigue en `202`; quedan `pendiente`; se abre el disyuntor y salen al volver |
| productos colgado | `docker compose pause productos` | `POST /pedidos` espera el timeout de pedidos (1 s por intento) y da `503` en 3 s |
| MongoDB de pedidos | `docker compose stop mongo-pedidos` | `POST /pedidos` da `503` en 2 s; el catálogo sigue andando |
| Inventario lento o con error | `INVENTARIO_MODO` (ver arriba) | El pedido queda `pendiente_confirmacion` y se reintenta |
| El gateway | `docker compose stop api-gateway` | No entra nada: es el único punto de entrada |

Para mostrar que una caída se detecta sola: dejar abierto http://localhost:9090/alerts, o Grafana, mientras está caída.

## Observabilidad

**Métricas** (Prometheus lee cada 15 s). Cualquier consulta, con el mismo formato:

```powershell
(Invoke-RestMethod $prom -Body @{ query = 'up' }).data.result | ForEach-Object { "$($_.metric.instance): $($_.value[1])" }   # 1 si Prometheus lee a cada servicio
(Invoke-RestMethod $prom -Body @{ query = 'sum by (estado) (rate(solicitudes_http_total{job="api-gateway"}[1m]))' }).data.result | ForEach-Object { "$($_.metric.estado): $($_.value[1])" }   # solicitudes por segundo, por código
(Invoke-RestMethod $prom -Body @{ query = 'histogram_quantile(0.95, sum by (le, job) (rate(duracion_solicitud_http_segundos_bucket[5m])))' }).data.result | ForEach-Object { "$($_.metric.job): $($_.value[1]) s" }   # latencia p95 por servicio
(Invoke-RestMethod $prom -Body @{ query = 'slo:notificaciones_post_rapidas:ratio_1h' }).data.result.value[1]   # objetivo: 99 % de los POST /notificaciones en menos de 300 ms
(Invoke-RestMethod $prom -Body @{ query = 'sum by (resultado) (pedidos_registrados_total)' }).data.result | ForEach-Object { "$($_.metric.resultado): $($_.value[1])" }   # pedidos aceptados y sin cupo
(Invoke-RestMethod $prom -Body @{ query = 'sum by (resultado) (notificaciones_envios_total)' }).data.result | ForEach-Object { "$($_.metric.resultado): $($_.value[1])" }   # envíos: enviada, reintento, fallida
```

Métricas propias de cada servicio: gateway (`gateway_solicitudes_total`, `gateway_instancia_sana`), productos (`cache_aciertos_total`, `cache_fallos_total`, `cache_errores_total`, `indice_pendiente_antiguedad_segundos`, `indice_sincronizaciones_total`), pedidos (`pedidos_registrados_total`, `pedidos_eventos_publicados_total`, `pedidos_inventario_llamadas_total`, `pedidos_circuito_inventario`, `pedidos_conexiones_tiempo_real`) y notificaciones (`notificaciones_envios_total`). Todos tienen `solicitudes_http_total` y `duracion_solicitud_http_segundos`.

**Alertas** ([`alertas.yml`](../observabilidad/prometheus/alertas.yml)):

```powershell
(Invoke-RestMethod 'http://localhost:9090/api/v1/rules').data.groups.rules | Where-Object type -EQ 'alerting' | Format-Table name, state, @{ n = 'condición'; e = { $_.query } } -Wrap   # todas, con su estado
(Invoke-RestMethod 'http://localhost:9090/api/v1/alerts').data.alerts | ForEach-Object { "$($_.labels.alertname): $($_.state) desde $($_.activeAt)" }   # las que están pendientes o disparadas
```

**Logs.** Cada servicio escribe una línea JSON por evento:

```powershell
docker compose logs --no-log-prefix --tail 10 api-gateway | ConvertFrom-Json | Format-Table time, level, metodo, ruta, estado, duracion_ms, trace_id -AutoSize
docker compose logs --no-log-prefix pedidos notificaciones-1 notificaciones-2 | ConvertFrom-Json -ErrorAction SilentlyContinue | Where-Object level -In 'WARN', 'ERROR' | Select-Object -Last 10 | Format-Table time, servicio, level, msg -AutoSize   # solo advertencias y errores
```

**Trazas.** Sacar el `trace_id` de la última solicitud de un tipo, ver sus logs en todos los servicios y abrirla en Jaeger:

```powershell
$ultima = docker compose logs --no-log-prefix api-gateway | ConvertFrom-Json -ErrorAction SilentlyContinue | Where-Object { $_.msg -eq 'solicitud atendida' -and $_.ruta -like '/notificaciones*' -and $_.trace_id } | Select-Object -Last 1
$ultima | Select-Object metodo, ruta, estado, trace_id
docker compose logs --no-log-prefix api-gateway pedidos notificaciones-1 notificaciones-2 productos usuarios | Select-String $ultima.trace_id   # la misma solicitud en todos los servicios
Start-Process "http://localhost:16686/trace/$($ultima.trace_id)"                                                                        # la traza en Jaeger
```

Cambiando `'/notificaciones*'` por `'/pedidos*'` se ve la traza de un pedido. Con un `POST /pedidos`, la traza sigue por RabbitMQ hasta notificaciones.

**Tablero:** Grafana, http://localhost:3000, tablero "Recién hecho": objetivo de latencia, solicitudes por segundo, errores 5xx, alertas disparadas, latencia p95, envíos por resultado, reparto por instancia, instancias en el balanceo y los paneles de carga de k6.

## Ver los datos en las bases

Con `$e` del bloque de [Preparar la terminal](#preparar-la-terminal):

```powershell
# MySQL de productos
docker compose exec mysql-productos mysql --default-character-set=utf8mb4 -u"$($e.PRODUCTOS_MYSQL_USUARIO)" -p"$($e.PRODUCTOS_MYSQL_CLAVE)" productos -e 'SHOW TABLES; SELECT nombre, precio_centavos, disponible FROM productos;'
# MySQL de usuarios (la clave se guarda con bcrypt, nunca en texto)
docker compose exec mysql-usuarios mysql --default-character-set=utf8mb4 -u"$($e.USUARIOS_MYSQL_USUARIO)" -p"$($e.USUARIOS_MYSQL_CLAVE)" usuarios -e 'SELECT email, rol FROM usuarios;'
# MongoDB de pedidos: colecciones, cupo de los últimos días y eventos sin publicar
docker compose exec mongo-pedidos mongosh --quiet -u "$($e.PEDIDOS_MONGO_USUARIO)" -p "$($e.PEDIDOS_MONGO_CLAVE)" --authenticationDatabase admin pedidos --eval 'db.getCollectionNames()'
docker compose exec mongo-pedidos mongosh --quiet -u "$($e.PEDIDOS_MONGO_USUARIO)" -p "$($e.PEDIDOS_MONGO_CLAVE)" --authenticationDatabase admin pedidos --eval 'db.dias.find().sort({_id:-1}).limit(5).toArray()'
docker compose exec mongo-pedidos mongosh --quiet -u "$($e.PEDIDOS_MONGO_USUARIO)" -p "$($e.PEDIDOS_MONGO_CLAVE)" --authenticationDatabase admin pedidos --eval 'db.eventos_pendientes.countDocuments()'
# MongoDB de notificaciones
docker compose exec mongo-notificaciones mongosh --quiet -u "$($e.NOTIFICACIONES_MONGO_USUARIO)" -p "$($e.NOTIFICACIONES_MONGO_CLAVE)" --authenticationDatabase admin notificaciones --eval 'db.notificaciones.find({}, {destinatario:1, estado:1, intentos:1}).sort({_id:-1}).limit(5).toArray()'
```

Los pedidos están en `pedidos`, el cupo por día en `dias` y la bandeja de salida en `eventos_pendientes`. Las notificaciones, en `notificaciones` y `claves_idempotencia`.

## Carga

k6 corre dentro del compose y manda sus resultados a Prometheus, así se ven en Grafana ([ADR-013](adr/ADR-013-capacidad-y-costos.md), [tests/carga](../tests/carga/README.md)). Escenarios: `catalogo.js`, `pedidos.js` y `notificaciones.js`.

**Caché prendida contra apagada:**

```powershell
$env:CACHE_HABILITADA = 'false'; docker compose up -d productos; $env:CACHE_HABILITADA = $null; Start-Sleep 5
docker compose --profile carga run --rm k6 run --vus 20 --duration 30s --tag testid=sin-cache /carga/catalogo.js
docker compose up -d productos; Start-Sleep 5
docker compose --profile carga run --rm k6 run --vus 20 --duration 30s --tag testid=con-cache /carga/catalogo.js
```

k6 imprime al final el p95 de cada ruta (en el ensayo, con caché: 7,3 ms el listado y 4,6 ms el detalle). En Grafana, los paneles "Test de carga" separan las corridas por `testid`. Para notificaciones, subir antes el límite (`$env:NOTIFICACIONES_LIMITE_POR_MINUTO = '100000'; docker compose up -d api-gateway; $env:NOTIFICACIONES_LIMITE_POR_MINUTO = $null`), porque si no da `429`. Los resultados completos de 10 minutos están en [ADR-013](adr/ADR-013-capacidad-y-costos.md#resultados).

## Tests

```powershell
./demo.ps1 tests unitarios               # go test de los cinco servicios y bun test del frontend
./demo.ps1 tests unitarios pedidos       # uno solo
./demo.ps1 tests integracion             # MySQL, MongoDB, Redis, Meilisearch y RabbitMQ reales en contenedores aparte; tarda unos 2,5 minutos y los borra al terminar
```

A mano, un servicio: `cd backend/pedidos; go test -count=1 -v ./internal/dominio/; cd ../..`. Sin las variables `*_TEST`, los tests de integración se saltean (`SKIP`) y corren solo los unitarios. Las variables que usa cada uno se imprimen al correr el atajo.

Los tests que más conviene mostrar: las transiciones de estado y el cupo en `backend/pedidos/internal/dominio`, el cupo con transacciones contra MongoDB real en `backend/pedidos/internal/adaptadores/mongodb`, la idempotencia en `backend/notificaciones/internal/aplicacion` y el disyuntor en `backend/notificaciones/internal/adaptadores/disyuntor`.

## Lo que todavía no está

Para no prometer lo que no hay:

- **Inventario real del otro grupo y su test de contrato** ([#77](https://github.com/mariavlindonc/recienhechoapp/issues/77)). El enunciado pide la capacidad del proveedor en el flujo principal y un test de contrato para la defensa. Hoy pedidos usa el simulador, y el servicio del otro grupo no estaba publicado (`404 no-server` el 06/11 y el 08/11).
- **Tests de punta a punta** (bonus B1, [#76](https://github.com/mariavlindonc/recienhechoapp/issues/76)).

## Si algo falla

| Síntoma | Qué hacer |
|---|---|
| `docker` no se reconoce o no responde | Abrir Docker Desktop y esperar *Engine running* |
| `levantar` falla con "container name is already in use" | `docker compose down` y volver a levantar |
| El frontend no llega al gateway | Hay otro programa en el puerto 5173 (un `bun run dev` olvidado): cerrarlo |
| Una solicitud da `503` | El servicio está caído, colgado o arrancando: `docker compose ps -a`. Si está `paused`, `docker compose unpause <servicio>` |
| Todo da `401` | Venció la sesión: volver a pegar el bloque de [Preparar la terminal](#preparar-la-terminal) |
| Iniciar sesión da `429` | 10 intentos por minuto: esperar un minuto |
| Una notificación queda en `pendiente` | Mailpit caído: `docker compose start mailpit` |
| La búsqueda no muestra un cambio | Tarda unos segundos; si no aparece, `docker compose exec productos /servicio reindexar` |
| Quedó una variable cambiada (caché apagada, modo de inventario) | `docker compose up -d productos pedidos api-gateway`: vuelven a los valores de `.env` |
| Las tildes se ven rotas en la terminal | `[Console]::OutputEncoding = [Text.Encoding]::UTF8` |
| Hay que empezar de cero con datos limpios | `docker compose down -v` borra **todos** los datos; después `./demo.ps1 levantar` y `./demo.ps1 cargar` |

## Al terminar

```powershell
./demo.ps1 detener      # baja todo; los datos quedan para la próxima vez
```
