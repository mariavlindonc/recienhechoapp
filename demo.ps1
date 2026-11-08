#requires -Version 7
<#
Atajos para la presentación: solo lo que es largo de tipear a mano. Todo lo demás
está como comando crudo en docs/DEMO.md. Cada atajo imprime en gris lo que corre.
Se usa desde la raíz del repo, en PowerShell 7 con Docker Desktop:

    ./demo.ps1 ayuda
#>
param(
	[Parameter(Position = 0)] [string] $Comando = 'ayuda',
	[Parameter(Position = 1, ValueFromRemainingArguments)] [string[]] $Argumentos = @()
)

$ErrorActionPreference = 'Stop'
$raiz = $PSScriptRoot
$archivoEnv = Join-Path $raiz '.env'
# GATEWAY se puede cambiar para pegarle a Render.
$gateway = $env:GATEWAY ?? 'http://localhost:8080'
$frontend = 'http://localhost:5173'

function Fallar([string] $mensaje) {
	Write-Host "FALLA: $mensaje" -ForegroundColor Red
	exit 1
}

function Mostrar([string] $texto) { Write-Host "> $texto" -ForegroundColor DarkGray }

function Compose {
	Mostrar "docker compose $($args -join ' ')"
	Push-Location $raiz
	try { docker compose @args } finally { Pop-Location }
	if ($LASTEXITCODE -ne 0) { Fallar "docker compose $($args -join ' ') terminó con error" }
}

# No se puede llamar Docker: PowerShell no distingue mayúsculas y taparía al comando docker.
function Correr-Docker {
	Mostrar "docker $($args -join ' ')"
	docker @args
	if ($LASTEXITCODE -ne 0) { Fallar "docker $($args -join ' ') terminó con error" }
}

function Valor([string] $nombre) {
	$linea = Get-Content $archivoEnv -ErrorAction SilentlyContinue | Where-Object { $_ -like "$nombre=*" } | Select-Object -First 1
	if ($linea) { return $linea.Substring($nombre.Length + 1) }
	return ''
}

# Sin .env, lo copia de .env.example. Con .env, le agrega al final las variables que
# falten, con el valor de ejemplo: lo que ya tiene no se toca. Así `docker compose`
# a secas funciona igual que este script.
function Completar-Env {
	$ejemplo = Join-Path $raiz '.env.example'
	if (-not (Test-Path $archivoEnv)) {
		Mostrar 'Copy-Item .env.example .env'
		Copy-Item $ejemplo $archivoEnv
		return
	}
	$definidas = Get-Content $archivoEnv | ForEach-Object { if ($_ -match '^([A-Z_]+)=') { $Matches[1] } }
	$faltantes = Get-Content $ejemplo | Where-Object { $_ -match '^([A-Z_]+)=' -and $Matches[1] -notin $definidas }
	if ($faltantes) {
		Add-Content -Path $archivoEnv -Value $faltantes
		Write-Host "Agregué a .env, con el valor de .env.example: $(($faltantes | ForEach-Object { ($_ -split '=')[0] }) -join ', ')" -ForegroundColor Yellow
	}
}

function Responde([string] $url) {
	try { return (Invoke-WebRequest $url -SkipHttpErrorCheck -TimeoutSec 5).StatusCode -eq 200 } catch { return $false }
}

function Esperar([string] $url, [int] $intentos = 60) {
	for ($intento = 0; $intento -lt $intentos; $intento++) {
		if (Responde $url) { return }
		Start-Sleep -Seconds 2
	}
	Fallar "no responde $url (ver: docker compose ps -a)"
}

# Cuerpo en UTF-8 explícito, para que las tildes lleguen bien.
function Enviar([string] $metodo, [string] $ruta, $cuerpo = $null, [hashtable] $encabezados = @{}) {
	$opciones = @{ Method = $metodo; Uri = "$gateway$ruta"; Headers = $encabezados; SkipHttpErrorCheck = $true }
	$texto = ''
	if ($null -ne $cuerpo) {
		$texto = $cuerpo -is [string] ? $cuerpo : ($cuerpo | ConvertTo-Json -Compress -Depth 5)
		$opciones.Body = [System.Text.Encoding]::UTF8.GetBytes($texto)
		$opciones.ContentType = 'application/json; charset=utf-8'
	}
	Mostrar "$metodo $ruta $texto"
	return Invoke-WebRequest @opciones
}

function Sesion([string] $email, [string] $clave) {
	$respuesta = Enviar POST /usuarios/sesion @{ email = $email; clave = $clave }
	if ($respuesta.StatusCode -ne 200) { Fallar "no se pudo iniciar sesión como ${email}: HTTP $($respuesta.StatusCode) (el gateway deja 10 intentos por minuto)" }
	return @{ Authorization = "Bearer $(($respuesta.Content | ConvertFrom-Json).token)" }
}

function Vendedor { return Sesion (Valor VENDEDOR_EMAIL) (Valor VENDEDOR_CLAVE) }

# Registra al comprador si no existe (409 si ya estaba) y entra.
function Comprador([string] $email) {
	Enviar POST /usuarios @{ email = $email; nombre = 'Comprador de prueba'; clave = 'comprador-demo' } | Out-Null
	return Sesion $email 'comprador-demo'
}

function Levantar {
	Completar-Env
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
	$encabezados = Vendedor
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
			Write-Host "  $($budin[0]): HTTP $($respuesta.StatusCode)"
		}
	}
	# Cupo para hoy y los próximos 7 días, así se puede encargar enseguida.
	foreach ($dias in 0..7) {
		Enviar PUT "/capacidad/$((Get-Date).AddDays($dias).ToString('yyyy-MM-dd'))" @{ capacidad = 10 } $encabezados | Out-Null
	}
	Write-Host 'OK budines y cupo cargados (la búsqueda los ve a los ~3 s)' -ForegroundColor Green
}

# concurrencia 10 3: pone cupo 3 en un día libre y manda 10 pedidos al mismo tiempo
# para ese día. Tienen que entrar 3 y el resto dar 409 sin_cupo (R1, ADR-015).
function Concurrencia([int] $cantidad, [int] $cupo) {
	$vendedor = Vendedor
	$comprador = Comprador 'comprador-concurrencia@recienhecho.test'
	# Un día lejano y al azar, para no chocar con pedidos de corridas anteriores.
	$fecha = (Get-Date).AddDays(100 + (Get-Random -Maximum 500)).ToString('yyyy-MM-dd')
	Enviar PUT "/capacidad/$fecha" @{ capacidad = $cupo } $vendedor | Out-Null
	$producto = (((Enviar GET /productos).Content | ConvertFrom-Json).productos | Select-Object -First 1)
	if (-not $producto) { Fallar 'no hay productos: ./demo.ps1 cargar' }
	$cuerpo = @{ tipo = 'programado'; fecha = $fecha; items = @(@{ productoId = $producto.id; cantidad = 1 }) } | ConvertTo-Json -Compress -Depth 5
	$url = "$gateway/pedidos"
	Mostrar "$cantidad x POST /pedidos $cuerpo   (todos a la vez, cada uno con su Idempotency-Key)"
	$codigos = 1..$cantidad | ForEach-Object -ThrottleLimit $cantidad -Parallel {
		$encabezados = $using:comprador + @{ 'Idempotency-Key' = [guid]::NewGuid().ToString() }
		(Invoke-WebRequest -Method Post $using:url -Headers $encabezados -ContentType 'application/json; charset=utf-8' -Body ([System.Text.Encoding]::UTF8.GetBytes($using:cuerpo)) -SkipHttpErrorCheck).StatusCode
	}
	Write-Host "Cupo $cupo para el $fecha, $cantidad pedidos a la vez:"
	$codigos | Group-Object | ForEach-Object {
		$significado = @{ '201' = 'aceptados'; '409' = 'rechazados por falta de cupo' }[$_.Name] ?? 'otro resultado'
		Write-Host "  $($_.Count) x HTTP $($_.Name) ($significado)"
	}
	$aceptados = @($codigos | Where-Object { $_ -eq 201 }).Count
	Write-Host ($aceptados -eq [Math]::Min($cupo, $cantidad) ? 'OK: no se vendió de más' : 'ATENCIÓN: entraron más o menos pedidos que el cupo') -ForegroundColor ($aceptados -eq [Math]::Min($cupo, $cantidad) ? 'Green' : 'Red')
}

$servicios = 'api-gateway', 'usuarios', 'productos', 'pedidos', 'notificaciones'

function Tests-Unitarios([string] $servicio) {
	foreach ($nombre in ($servicio ? @($servicio) : $servicios)) {
		Write-Host "=== $nombre" -ForegroundColor Cyan
		Push-Location (Join-Path $raiz "backend/$nombre")
		Mostrar "cd backend/$nombre; go test -count=1 ./..."
		try { go test -count=1 ./... } finally { Pop-Location }
	}
	if (-not $servicio) {
		Write-Host '=== frontend' -ForegroundColor Cyan
		Push-Location (Join-Path $raiz 'frontend')
		Mostrar 'cd frontend; bun test'
		try { bun test } finally { Pop-Location }
	}
}

# Levanta las bases en contenedores aparte (otros puertos que el compose, así no se
# pisan), corre los tests de integración de cada servicio contra ellas y las borra.
function Tests-Integracion {
	$contenedores = 'prueba-mysql', 'prueba-mongo', 'prueba-redis', 'prueba-meili', 'prueba-rabbit'
	docker rm -f @contenedores 2>$null | Out-Null
	try {
		Correr-Docker run -d --name prueba-mysql -p 127.0.0.1:13307:3306 -e MYSQL_ROOT_PASSWORD=prueba -e MYSQL_DATABASE=pruebas mysql:8.4.10
		Correr-Docker run -d --name prueba-mongo -p 127.0.0.1:27031:27017 mongo:8 --replSet rs0 --bind_ip_all
		Correr-Docker run -d --name prueba-redis -p 127.0.0.1:16381:6379 redis:7.4.11
		Correr-Docker run -d --name prueba-meili -p 127.0.0.1:17702:7700 -e MEILI_MASTER_KEY=clave-de-prueba-de-16 getmeili/meilisearch:v1.54.3
		Correr-Docker run -d --name prueba-rabbit -p 127.0.0.1:15673:5672 -e RABBITMQ_DEFAULT_USER=prueba -e RABBITMQ_DEFAULT_PASS=prueba rabbitmq:4.3.5-management-alpine
		Write-Host 'Esperando a que arranquen (MySQL y RabbitMQ tardan unos 20 s)...'
		foreach ($intento in 1..60) {
			docker exec prueba-mysql mysqladmin ping -h127.0.0.1 --protocol=tcp -uroot -pprueba --silent 2>$null | Out-Null
			if ($LASTEXITCODE -eq 0) { break }
			Start-Sleep -Seconds 2
		}
		foreach ($intento in 1..30) {
			docker exec prueba-mongo mongosh --quiet --eval "db.adminCommand('ping').ok" 2>$null | Out-Null
			if ($LASTEXITCODE -eq 0) { break }
			Start-Sleep -Seconds 1
		}
		# pedidos usa transacciones: MongoDB tiene que ser un replica set.
		Correr-Docker exec prueba-mongo mongosh --quiet --eval 'rs.initiate()' | Out-Null
		foreach ($intento in 1..60) {
			docker exec prueba-rabbit rabbitmq-diagnostics -q ping 2>$null | Out-Null
			if ($LASTEXITCODE -eq 0) { break }
			Start-Sleep -Seconds 2
		}
		$variables = @{
			'usuarios'       = @{ MYSQL_DSN_TEST = 'root:prueba@tcp(127.0.0.1:13307)/pruebas?parseTime=true' }
			'productos'      = @{ MYSQL_DSN_TEST = 'root:prueba@tcp(127.0.0.1:13307)/pruebas?parseTime=true'; REDIS_URL_TEST = 'redis://127.0.0.1:16381/0'; MEILI_URL_TEST = 'http://127.0.0.1:17702'; MEILI_CLAVE_TEST = 'clave-de-prueba-de-16' }
			'pedidos'        = @{ MONGO_URI_TEST = 'mongodb://127.0.0.1:27031/?directConnection=true'; RABBITMQ_URI_TEST = 'amqp://prueba:prueba@127.0.0.1:15673/' }
			'notificaciones' = @{ MONGO_URI_TEST = 'mongodb://127.0.0.1:27031/?directConnection=true'; REDIS_URL_TEST = 'redis://127.0.0.1:16381/1' }
		}
		foreach ($servicio in 'usuarios', 'productos', 'pedidos', 'notificaciones') {
			Write-Host "=== $servicio" -ForegroundColor Cyan
			foreach ($par in $variables[$servicio].GetEnumerator()) { [Environment]::SetEnvironmentVariable($par.Key, $par.Value) }
			Push-Location (Join-Path $raiz "backend/$servicio")
			Mostrar "cd backend/$servicio; $(($variables[$servicio].GetEnumerator() | ForEach-Object { "`$env:$($_.Key)='$($_.Value)'" }) -join '; '); go test -count=1 -v ./... | Select-String -- '--- (PASS|FAIL|SKIP)'"
			try {
				go test -count=1 -v ./... | Select-String -Pattern '^\s*--- (PASS|FAIL|SKIP)|^(ok|FAIL)\s'
			} finally {
				Pop-Location
				foreach ($par in $variables[$servicio].GetEnumerator()) { [Environment]::SetEnvironmentVariable($par.Key, $null) }
			}
		}
	} finally {
		Mostrar "docker rm -f $($contenedores -join ' ')"
		docker rm -f @contenedores 2>$null | Out-Null
	}
}

function Ayuda {
	@'
Atajos (lo demás, como comando crudo, en docs/DEMO.md):

  ./demo.ps1 levantar                  completa .env, levanta todo y espera al gateway y al frontend
  ./demo.ps1 cargar                    categorías, tres budines y cupo de 10 para hoy y 7 días más
  ./demo.ps1 concurrencia [N] [cupo]   N pedidos a la vez para un día con ese cupo (por defecto 10 y 3)
  ./demo.ps1 tests unitarios [servicio]   go test de cada servicio y bun test del frontend
  ./demo.ps1 tests integracion         bases reales en contenedores aparte, tests y las borra
  ./demo.ps1 detener                   baja todo (los datos quedan)
'@ | Write-Host
}

switch ($Comando) {
	'levantar' { Levantar }
	'cargar' { Cargar }
	'concurrencia' { Concurrencia ($Argumentos[0] ? [int] $Argumentos[0] : 10) ($Argumentos[1] ? [int] $Argumentos[1] : 3) }
	'tests' {
		switch ($Argumentos[0]) {
			'unitarios' { Tests-Unitarios ($Argumentos[1] ?? '') }
			'integracion' { Tests-Integracion }
			default { Fallar 'uso: ./demo.ps1 tests unitarios [servicio] | ./demo.ps1 tests integracion' }
		}
	}
	'detener' { Compose down; Write-Host 'OK todo abajo (los datos quedan para la próxima)' -ForegroundColor Green }
	{ $_ -in 'ayuda', '-h', '--help' } { Ayuda }
	default { Ayuda; exit 2 }
}
