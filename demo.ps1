#requires -Version 7
<#
Panel de comandos para la presentación (docs/DEMO.md). Se corre desde la raíz del
repo, en PowerShell 7 con Docker Desktop:

    ./demo.ps1 ayuda
#>
param(
	[Parameter(Position = 0)] [string] $Comando = 'ayuda',
	[Parameter(Position = 1, ValueFromRemainingArguments)] [string[]] $Argumentos = @()
)

$ErrorActionPreference = 'Stop'
$raiz = $PSScriptRoot
# Relativo a la raíz, con barras normales: lo entienden Docker y el sistema de archivos.
$entornoRelativo = '.claude/skills/run-recienhechoapp/.tmp/env'
$entorno = Join-Path $raiz $entornoRelativo
$archivoToken = Join-Path $raiz '.claude/skills/run-recienhechoapp/.tmp/token'
# GATEWAY y CLAVE_API se pueden cambiar para pegarle a Render.
$gateway = $env:GATEWAY ?? 'http://localhost:8080'
$frontend = 'http://localhost:5173'

function Fallar([string] $mensaje) {
	Write-Host "FALLA: $mensaje" -ForegroundColor Red
	exit 1
}

function Compose {
	if (-not (Test-Path $entorno)) { Fallar 'primero: ./demo.ps1 levantar' }
	Push-Location $raiz
	try { docker compose --env-file $entornoRelativo @args } finally { Pop-Location }
	if ($LASTEXITCODE -ne 0) { Fallar "docker compose $($args -join ' ') terminó con error" }
}

function Valor([string] $nombre) {
	$linea = Get-Content $entorno -ErrorAction SilentlyContinue | Where-Object { $_ -like "$nombre=*" } | Select-Object -First 1
	if ($linea) { return $linea.Substring($nombre.Length + 1) }
	return ''
}

# El .env no se toca: se copia y se le agrega lo que falte de .env.example.
function Armar-Entorno {
	New-Item -ItemType Directory -Force (Split-Path $entorno) | Out-Null
	$lineas = [System.Collections.Generic.List[string]]::new()
	$archivoPropio = Join-Path $raiz '.env'
	if (Test-Path $archivoPropio) { $lineas.AddRange([string[]](Get-Content $archivoPropio)) }
	$definidas = $lineas | ForEach-Object { if ($_ -match '^([A-Z_]+)=') { $Matches[1] } }
	foreach ($linea in Get-Content (Join-Path $raiz '.env.example')) {
		if ($linea -match '^([A-Z_]+)=' -and $Matches[1] -notin $definidas) { $lineas.Add($linea) }
	}
	Set-Content -Path $entorno -Value $lineas
}

function Responde([string] $url) {
	try { return (Invoke-WebRequest $url -SkipHttpErrorCheck -TimeoutSec 5).StatusCode -eq 200 } catch { return $false }
}

function Esperar([string] $url, [int] $intentos = 60) {
	for ($intento = 0; $intento -lt $intentos; $intento++) {
		if (Responde $url) { return }
		Start-Sleep -Seconds 2
	}
	Fallar "no responde $url (ver: ./demo.ps1 estado)"
}

# Cuerpo en UTF-8 explícito, para que los acentos lleguen bien.
function Enviar([string] $metodo, [string] $ruta, $cuerpo = $null, [hashtable] $encabezados = @{}) {
	$opciones = @{ Method = $metodo; Uri = "$gateway$ruta"; Headers = $encabezados; SkipHttpErrorCheck = $true }
	if ($null -ne $cuerpo) {
		$texto = $cuerpo -is [string] ? $cuerpo : ($cuerpo | ConvertTo-Json -Compress -Depth 5)
		$opciones.Body = [System.Text.Encoding]::UTF8.GetBytes($texto)
		$opciones.ContentType = 'application/json; charset=utf-8'
	}
	return Invoke-WebRequest @opciones
}

# El token del vendedor se guarda: iniciar sesión en cada comando gastaría los 10
# intentos por minuto que deja el gateway (ADR-019). El token vence a la hora: pasados
# 50 minutos se pide otro.
function Token {
	if ((Test-Path $archivoToken) -and (Get-Item $archivoToken).LastWriteTime -gt (Get-Date).AddMinutes(-50)) {
		return Get-Content $archivoToken -Raw
	}
	$respuesta = Enviar POST /usuarios/sesion @{ email = (Valor VENDEDOR_EMAIL); clave = (Valor VENDEDOR_CLAVE) }
	if ($respuesta.StatusCode -ne 200) { Fallar "no se pudo iniciar sesión como vendedor: HTTP $($respuesta.StatusCode)" }
	$token = ($respuesta.Content | ConvertFrom-Json).token
	Set-Content -Path $archivoToken -Value $token -NoNewline
	return $token
}

function ConSesion { return @{ Authorization = "Bearer $(Token)" } }

# La primera clave de NOTIFICACIONES_CLAVES_API (grupo:clave,...), o CLAVE_API.
function ClaveApi {
	$clave = $env:CLAVE_API ?? ((Valor NOTIFICACIONES_CLAVES_API) -split ',')[0] -replace '^[^:]*:', ''
	if (-not $clave) { Fallar 'no hay clave de API: definí $env:CLAVE_API' }
	return $clave
}

function Levantar {
	Armar-Entorno
	Remove-Item $archivoToken -ErrorAction SilentlyContinue
	Compose up --build -d
	Esperar "$gateway/categorias"
	Esperar "$frontend/"
	# Un "bun run dev" olvidado escucha en el 5173 y le gana al contenedor.
	if (-not (Invoke-WebRequest "$frontend/api/categorias" -SkipHttpErrorCheck).Content.StartsWith('[')) {
		Fallar "$frontend/api no llega al gateway: hay otro proceso en el 5173"
	}
	Write-Host "OK gateway en $gateway, frontend en $frontend, Mailpit en http://localhost:8025" -ForegroundColor Green
}

function Cargar {
	$encabezados = ConSesion
	$categorias = (Enviar GET /categorias).Content | ConvertFrom-Json
	foreach ($nombre in 'Zanahoria', 'Cítricos', 'Sin harina de trigo') {
		if ($nombre -notin $categorias.nombre) { Enviar POST /categorias @{ nombre = $nombre } $encabezados | Out-Null }
	}
	$categorias = (Enviar GET /categorias).Content | ConvertFrom-Json
	$idDe = { param($nombre) ($categorias | Where-Object nombre -EQ $nombre).id }

	if ((Enviar GET /productos).Content -match 'Budín de limón') {
		Write-Host 'los budines ya estaban'
	} else {
		# Precios de relleno a propósito: no sabemos los reales.
		$budines = @(
			@('Budín de limón', 'Con semillas de amapola y limones caramelizados.', 'Cítricos', 12312300, $false),
			@('Budín de zanahoria', 'Con frutos secos y glasé de naranja.', 'Zanahoria', 45645600, $false),
			@('Budín de zanahoria sin TACC', 'Masa de zanahoria sin harina de trigo, con cranberries.', 'Sin harina de trigo', 78978900, $true)
		)
		foreach ($budin in $budines) {
			$respuesta = Enviar POST /productos @{
				nombre = $budin[0]; descripcion = $budin[1]; categoriaId = (& $idDe $budin[2])
				precioCentavos = $budin[3]; anticipacionMinimaDias = 1; aptoCeliacos = $budin[4]; disponible = $true
			} $encabezados
			Write-Host "  $($budin[0]): $($respuesta.StatusCode)"
		}
	}
	# Cupo para hoy y los próximos 7 días, así se puede encargar enseguida.
	foreach ($dias in 0..7) {
		Enviar PUT "/capacidad/$((Get-Date).AddDays($dias).ToString('yyyy-MM-dd'))" @{ capacidad = 10 } $encabezados | Out-Null
	}
	Write-Host 'OK budines y cupo cargados (la búsqueda los ve a los ~3 s)' -ForegroundColor Green
}

# Reinicia un servicio con una variable cambiada. La variable del proceso le gana
# al archivo de entorno: el .env no se toca.
function Reiniciar-Con([string] $servicio, [string] $variable, [string] $valor) {
	[Environment]::SetEnvironmentVariable($variable, $valor)
	try { Compose up -d $servicio } finally { [Environment]::SetEnvironmentVariable($variable, $null) }
	Esperar "$gateway/categorias" 30
}

function Cache([string] $accion) {
	switch ($accion) {
		'estado' {
			# Contadores desde que arrancó productos, leídos de Prometheus.
			foreach ($metrica in 'cache_aciertos_total', 'cache_fallos_total') {
				Write-Host "${metrica}:"
				$consulta = [uri]::EscapeDataString("sum by (tipo) ($metrica)")
				foreach ($serie in (Invoke-RestMethod "http://localhost:9090/api/v1/query?query=$consulta").data.result) {
					Write-Host "  $($serie.metric.tipo): $($serie.value[1])"
				}
			}
			return
		}
		'apagar' { $habilitada = 'false' }
		'prender' { $habilitada = 'true' }
		default { Fallar 'uso: ./demo.ps1 cache apagar|prender|estado' }
	}
	Reiniciar-Con productos CACHE_HABILITADA $habilitada
	Start-Sleep -Seconds 1
	$arranque = (Compose logs --tail 50 productos) | Select-String '"cache":(true|false)' | Select-Object -Last 1
	Write-Host "productos arrancó con $($arranque.Matches[0].Value)" -ForegroundColor Green
}

function Inventario([string] $modo) {
	if ($modo -notin 'confirma', 'sin_stock', 'lento', 'error', 'rechazo') {
		Fallar 'uso: ./demo.ps1 inventario confirma|sin_stock|lento|error|rechazo'
	}
	Reiniciar-Con pedidos INVENTARIO_MODO $modo
	Write-Host "pedidos arrancó con el inventario falso en modo $modo" -ForegroundColor Green
}

function Servicio-Valido([string] $servicio) {
	if (-not $servicio) { Fallar 'falta el nombre del servicio: ./demo.ps1 servicios' }
	if ($servicio -notin (Compose config --services)) { Fallar "no existe el servicio ${servicio}: ./demo.ps1 servicios" }
}

# pedir GET /productos  o  pedir POST /categorias '{"nombre":"Frutales"}'
# Va con el token del vendedor; con $env:SIN_SESION = '1' va sin token.
function Pedir([string] $metodo, [string] $ruta, [string] $cuerpo) {
	if (-not $metodo -or -not $ruta) { Fallar "uso: ./demo.ps1 pedir METODO /ruta ['{json}']" }
	$encabezados = $env:SIN_SESION -eq '1' ? @{} : (ConSesion)
	$respuesta = Enviar $metodo $ruta ($cuerpo ? $cuerpo : $null) $encabezados
	Write-Host $respuesta.Content
	Write-Host "HTTP $($respuesta.StatusCode)" -ForegroundColor Cyan
}

# notificar correo@ejemplo.com: crea una notificación y sigue su estado hasta que
# sale. Con $env:IDEMPOTENCIA se repite la misma clave, para mostrar la idempotencia.
function Notificar([string] $destinatario, [string] $asunto, [string] $cuerpo) {
	if (-not $destinatario) { Fallar 'uso: ./demo.ps1 notificar correo@ejemplo.com' }
	$clave = ClaveApi
	$idempotencia = $env:IDEMPOTENCIA ?? "demo-$([guid]::NewGuid())"
	$respuesta = Enviar POST /notificaciones @{ canal = 'email'; destinatario = $destinatario; asunto = ($asunto ? $asunto : 'Notificación de prueba'); cuerpo = ($cuerpo ? $cuerpo : 'Mensaje de prueba del servicio de notificaciones.') } @{ 'X-API-Key' = $clave; 'Idempotency-Key' = $idempotencia }
	Write-Host $respuesta.Content
	Write-Host "HTTP $($respuesta.StatusCode)" -ForegroundColor Cyan
	if ($respuesta.StatusCode -ne 202) { return }
	$id = ($respuesta.Content | ConvertFrom-Json).id
	foreach ($intento in 1..15) {
		$estado = ((Enviar GET "/notificaciones/$id" $null @{ 'X-API-Key' = $clave }).Content | ConvertFrom-Json).estado
		Write-Host "estado: $estado"
		if ($estado -ne 'pendiente') { return }
		Start-Sleep -Seconds 2
	}
}

# rafaga 70: manda N notificaciones seguidas y cuenta las respuestas. Pasado el
# límite de 60 por minuto aparecen los 429 (ADR-004); el reparto entre instancias
# se ve en Grafana.
function Rafaga([int] $cantidad) {
	$clave = ClaveApi
	$cuerpo = @{ canal = 'email'; destinatario = 'cliente@ejemplo.com'; asunto = 'Ráfaga'; cuerpo = 'Prueba de límite.' }
	$codigos = foreach ($numero in 1..$cantidad) {
		(Enviar POST /notificaciones $cuerpo @{ 'X-API-Key' = $clave; 'Idempotency-Key' = "rafaga-$([guid]::NewGuid())" }).StatusCode
	}
	$codigos | Group-Object | ForEach-Object { Write-Host "  $($_.Count) respuestas $($_.Name)" }
}

function Ayuda {
	@'
Uso: ./demo.ps1 <comando>

  levantar | cargar | estado | detener   la app completa
  abrir                                  las direcciones de cada herramienta

  cache apagar | prender | estado        reinicia productos con o sin caché; estado: aciertos y fallos
  inventario <modo>                      confirma, sin_stock, lento, error o rechazo (reinicia pedidos)
  caer <servicio> | volver <servicio>    detiene o vuelve a levantar un contenedor
  servicios                              los nombres que aceptan caer y volver
  colas                                  mensajes en las colas de RabbitMQ (incluida la DLQ)
  reindexar                              reconstruye el índice de búsqueda (ADR-008)
  carga                                  30 s de carga sobre el catálogo (para comparar con y sin caché)

  pedir METODO /ruta ['{json}']          una solicitud al gateway como vendedor ($env:SIN_SESION='1': sin token)
  notificar correo [asunto] [cuerpo]     crea una notificación (con el texto que quieras) y sigue su estado
  rafaga [cantidad]                      muchas notificaciones seguidas: muestra el límite por minuto

Para Render:  $env:GATEWAY='https://...'; $env:CLAVE_API='...'; ./demo.ps1 notificar correo@ejemplo.com
'@ | Write-Host
}

switch ($Comando) {
	'levantar' { Levantar }
	'cargar' { Cargar }
	'estado' { Compose ps -a }
	'detener' { Compose down; Write-Host 'OK compose abajo (los volúmenes quedan)' -ForegroundColor Green }
	'abrir' {
		@'
Frontend      http://localhost:5173
Gateway       http://localhost:8080
Grafana       http://localhost:3000   (tablero "Recién hecho")
Jaeger        http://localhost:16686
Prometheus    http://localhost:9090/alerts
RabbitMQ      http://localhost:15672
Mailpit       http://localhost:8025
Meilisearch   http://localhost:7700
'@ | Write-Host
	}
	'cache' { Cache ($Argumentos[0] ?? '') }
	'inventario' { Inventario ($Argumentos[0] ?? '') }
	'caer' { Servicio-Valido ($Argumentos[0] ?? ''); Compose stop $Argumentos[0] }
	'volver' { Servicio-Valido ($Argumentos[0] ?? ''); Compose start $Argumentos[0] }
	'servicios' { Compose config --services }
	'colas' { Compose exec rabbitmq rabbitmqctl list_queues name messages }
	'reindexar' { Compose exec productos /servicio reindexar }
	# 30 s del test del catálogo (la corrida completa dura 10 min). Se ve en Grafana,
	# en los paneles "Test de carga", separada por testid.
	'carga' {
		$estadoCache = ((Compose logs --tail 200 productos) | Select-String '"cache":(true|false)' | Select-Object -Last 1).Matches[0].Groups[1].Value
		Compose --profile carga run --rm k6 run --vus 20 --duration 30s --tag "testid=catalogo-cache-$estadoCache-$(Get-Date -Format HHmmss)" /carga/catalogo.js
	}
	'pedir' { Pedir ($Argumentos[0] ?? '') ($Argumentos[1] ?? '') ($Argumentos[2] ?? '') }
	'notificar' { Notificar ($Argumentos[0] ?? '') ($Argumentos[1] ?? '') ($Argumentos[2] ?? '') }
	'rafaga' { Rafaga ($Argumentos[0] ? [int] $Argumentos[0] : 70) }
	{ $_ -in 'ayuda', '-h', '--help' } { Ayuda }
	default { Ayuda; exit 2 }
}
