// Maneja el frontend con Chrome sin ventana, por el protocolo de DevTools. Sin dependencias:
// Bun trae fetch y WebSocket. Uso (desde la raíz del repo, con la app levantada con app.sh):
//   bun .claude/skills/run-recienhechoapp/navegador.ts captura <ruta> [oscuro]
//   bun .claude/skills/run-recienhechoapp/navegador.ts eval <ruta> '<expresión JS>'
//   bun .claude/skills/run-recienhechoapp/navegador.ts flujo
// Las capturas quedan en .claude/skills/run-recienhechoapp/.tmp/capturas/.
import { existsSync, mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

const FRONTEND = process.env.FRONTEND_URL ?? 'http://localhost:5173'
const PUERTO_DEPURACION = 9333
const CARPETA = join(import.meta.dir, '.tmp')
const CAPTURAS = join(CARPETA, 'capturas')
mkdirSync(CAPTURAS, { recursive: true })

const CANDIDATOS = [
  process.env.NAVEGADOR,
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  '/usr/bin/chromium',
  '/usr/bin/google-chrome',
].filter(Boolean) as string[]

const ejecutable = CANDIDATOS.find(ruta => existsSync(ruta))
if (!ejecutable) throw new Error(`no encontré Chrome; pasalo en NAVEGADOR. Probé: ${CANDIDATOS.join(', ')}`)

const chrome = Bun.spawn(
  [
    ejecutable,
    '--headless=new',
    `--remote-debugging-port=${PUERTO_DEPURACION}`,
    `--user-data-dir=${join(CARPETA, 'perfil-chrome')}`,
    '--window-size=1280,860',
    '--no-first-run',
    'about:blank',
  ],
  { stdout: 'ignore', stderr: 'ignore' },
)

const esperar = (milisegundos: number) => new Promise(resolver => setTimeout(resolver, milisegundos))

async function conectar() {
  for (let intento = 0; intento < 50; intento++) {
    const pestanias = (await fetch(`http://127.0.0.1:${PUERTO_DEPURACION}/json/list`).then(respuesta => respuesta.json()).catch(() => [])) as { type: string; webSocketDebuggerUrl: string }[]
    const pagina = pestanias.find(pestania => pestania.type === 'page')
    if (pagina) return pagina.webSocketDebuggerUrl
    await esperar(200)
  }
  throw new Error('Chrome no abrió el puerto de depuración')
}

const socket = new WebSocket(await conectar())
await new Promise(resolver => socket.addEventListener('open', resolver, { once: true }))
let siguienteId = 0
const pendientes = new Map<number, { resolver: (valor: any) => void; rechazar: (error: Error) => void }>()
const alCargar: (() => void)[] = []
socket.addEventListener('message', evento => {
  const mensaje = JSON.parse(String(evento.data))
  if (mensaje.method === 'Page.loadEventFired') alCargar.splice(0).forEach(avisar => avisar())
  const pendiente = pendientes.get(mensaje.id)
  if (!pendiente) return
  pendientes.delete(mensaje.id)
  if (mensaje.error) pendiente.rechazar(new Error(mensaje.error.message))
  else pendiente.resolver(mensaje.result)
})

function enviar(metodo: string, parametros: object = {}): Promise<any> {
  const id = ++siguienteId
  socket.send(JSON.stringify({ id, method: metodo, params: parametros }))
  return new Promise((resolver, rechazar) => pendientes.set(id, { resolver, rechazar }))
}

async function ir(ruta: string) {
  const cargada = new Promise<void>(resolver => alCargar.push(resolver))
  await enviar('Page.navigate', { url: new URL(ruta, FRONTEND).href })
  await cargada
  await esperar(800) // React pide los datos después de cargar la página.
}

// Evalúa en la página; acepta await. Devuelve el valor ya serializado.
async function evaluar(expresion: string) {
  const resultado = await enviar('Runtime.evaluate', { expression: `(async () => (${expresion}))()`, awaitPromise: true, returnByValue: true })
  if (resultado.exceptionDetails) throw new Error(resultado.exceptionDetails.exception?.description ?? resultado.exceptionDetails.text)
  return resultado.result.value
}

async function capturar(nombre: string) {
  const { data } = await enviar('Page.captureScreenshot', { format: 'png' })
  const archivo = join(CAPTURAS, `${nombre}.png`)
  writeFileSync(archivo, Buffer.from(data, 'base64'))
  console.log(`captura: ${archivo}`)
}

const nombreDeArchivo = (ruta: string) => ruta.replace(/[^a-z0-9]+/gi, '-').replace(/^-|-$/g, '') || 'inicio'

// Completa un md-text-field por su etiqueta: los componentes de material-esm no son inputs comunes.
const LLENAR = `(etiqueta, valor) => {
  const campo = [...document.querySelectorAll('md-text-field')].find(elemento => elemento.label?.startsWith(etiqueta))
  if (!campo) throw new Error('no hay campo ' + etiqueta)
  campo.value = valor
  campo.dispatchEvent(new InputEvent('input', { bubbles: true, composed: true }))
}`

async function flujo() {
  const email = `flujo${Date.now()}@ejemplo.com`
  await ir('/registrarse')
  await evaluar(`(() => { const llenar = ${LLENAR}; llenar('Nombre', 'Lucía'); llenar('Email', '${email}'); llenar('Contraseña', 'clave-segura') })()`)
  await esperar(200)
  await evaluar(`document.querySelector('form').requestSubmit()`)
  await esperar(2000)
  console.log('registro:', await evaluar(`location.pathname + ' sesion=' + !!sessionStorage.getItem('sesion')`))
  await capturar('flujo-1-catalogo')
  await evaluar(`[...document.querySelectorAll('md-card a')].find(enlace => enlace.textContent.includes('Ver producto')).click()`)
  await esperar(1500)
  await capturar('flujo-2-detalle')
  await evaluar(`[...document.querySelectorAll('main a')].find(enlace => enlace.textContent.includes('Encargar')).click()`)
  await esperar(1500)
  const manana = new Date(Date.now() + 864e5).toISOString().slice(0, 10)
  await evaluar(`(() => { const llenar = ${LLENAR}; llenar('Fecha', '${manana}') })()`)
  await esperar(200)
  await evaluar(`document.querySelector('form').requestSubmit()`)
  await esperar(3000)
  const resultado = await evaluar(`({ ruta: location.pathname, resaltado: !!document.querySelector('.recien-creado'), error: document.querySelector('.aviso-error')?.textContent ?? null, pedidos: document.querySelector('main').innerText.replace(/\\n+/g, ' / ').slice(0, 200) })`)
  console.log('pedido:', JSON.stringify(resultado))
  await capturar('flujo-3-mis-pedidos')
  if (resultado.ruta !== '/mis-pedidos' || resultado.error) throw new Error('el flujo no terminó en Mis pedidos')
}

try {
  await enviar('Page.enable')
  const [comando, ruta = '/', extra] = process.argv.slice(2)
  // Git Bash convierte "/mis-pedidos" en "C:/Program Files/Git/mis-pedidos" al llamar a un .exe.
  if (/^[a-z]:[\\/]/i.test(ruta)) throw new Error(`la ruta llegó como ${ruta}: correlo con MSYS_NO_PATHCONV=1`)
  // Chrome sin ventana copia el tema de Windows: si está en oscuro, todo sale oscuro. Se fija siempre.
  await enviar('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: extra === 'oscuro' ? 'dark' : 'light' }] })
  if (comando === 'captura') {
    await ir(ruta)
    await capturar(`${nombreDeArchivo(ruta)}${extra === 'oscuro' ? '-oscuro' : ''}`)
  } else if (comando === 'eval') {
    await ir(ruta)
    console.log(JSON.stringify(await evaluar(extra ?? 'document.title'), null, 2))
  } else if (comando === 'flujo') {
    await flujo()
    console.log('OK flujo del comprador completo')
  } else {
    throw new Error('uso: navegador.ts captura <ruta> [oscuro] | eval <ruta> <js> | flujo')
  }
} finally {
  socket.close()
  chrome.kill()
}
