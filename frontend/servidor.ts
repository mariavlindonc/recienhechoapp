// Sirve la app compilada y reenvía /api/* al api-gateway, así el navegador no habla con otros orígenes (ADR-001).
const problemas = []
const puerto = Number(process.env.PORT)
if (!Number.isInteger(puerto) || puerto <= 0) problemas.push('PORT debe ser un número de puerto')
const destinoApi = process.env.API_GATEWAY_URL
if (!destinoApi) problemas.push('API_GATEWAY_URL es obligatoria')
if (problemas.length > 0) {
  console.error(`configuración inválida:\n- ${problemas.join('\n- ')}`)
  process.exit(1)
}

const carpetaCompilada = new URL('./dist/', import.meta.url)

// El WebSocket del seguimiento en vivo (ADR-018): fetch no sirve para reenviarlo, así que el
// servidor hace de puente entre el navegador y el api-gateway.
const RUTA_TIEMPO_REAL = '/api/pedidos/tiempo-real'
type Puente = { destino: string; cookie: string; gateway?: WebSocket }

const servidor = Bun.serve<Puente>({
  port: puerto,
  async fetch(pedido, servidorActual) {
    const url = new URL(pedido.url)
    if (url.pathname === RUTA_TIEMPO_REAL && pedido.headers.get('upgrade')?.toLowerCase() === 'websocket') {
      // Con la sesión en una cookie, otro sitio no puede abrir este WebSocket: además de
      // SameSite, el puente acepta solo a nuestra propia página (ADR-019).
      const origen = pedido.headers.get('origin')
      if (origen && new URL(origen).host !== pedido.headers.get('host')) return new Response('Origen no permitido', { status: 403 })
      const destino = new URL(url.pathname.replace(/^\/api/, ''), destinoApi)
      destino.protocol = destino.protocol === 'https:' ? 'wss:' : 'ws:'
      if (servidorActual.upgrade(pedido, { data: { destino: destino.href, cookie: pedido.headers.get('cookie') ?? '' } })) return undefined
      return new Response('No se pudo abrir el WebSocket', { status: 400 })
    }
    if (url.pathname.startsWith('/api/')) {
      const destino = new URL(url.pathname.replace(/^\/api/, '') + url.search, destinoApi)
      return fetch(destino, { method: pedido.method, headers: pedido.headers, body: pedido.body, redirect: 'manual' })
    }
    const archivo = Bun.file(new URL(`.${url.pathname}`, carpetaCompilada))
    if (url.pathname !== '/' && (await archivo.exists())) return new Response(archivo)
    // Rutas del SPA: cualquier otra cosa devuelve la app.
    return new Response(Bun.file(new URL('index.html', carpetaCompilada)), { headers: { 'content-type': 'text/html; charset=utf-8' } })
  },
  websocket: {
    open(navegador) {
      // La cookie sigue hasta el gateway, que es el que verifica la sesión. Bun deja mandar
      // headers al abrir un WebSocket, aunque los tipos del navegador no lo conozcan.
      const gateway = new WebSocket(navegador.data.destino, { headers: { Cookie: navegador.data.cookie } } as never)
      navegador.data.gateway = gateway
      gateway.onmessage = evento => navegador.send(String(evento.data))
      gateway.onclose = () => navegador.close()
    },
    // El navegador no manda mensajes: solo escucha.
    message() {},
    close(navegador) {
      navegador.data.gateway?.close()
    },
  },
})

process.on('SIGTERM', () => {
  servidor.stop()
  process.exit(0)
})

console.log(`frontend en http://localhost:${puerto}`)
