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

const servidor = Bun.serve({
  port: puerto,
  async fetch(pedido) {
    const url = new URL(pedido.url)
    if (url.pathname.startsWith('/api/')) {
      const destino = new URL(url.pathname.replace(/^\/api/, '') + url.search, destinoApi)
      return fetch(destino, { method: pedido.method, headers: pedido.headers, body: pedido.body, redirect: 'manual' })
    }
    const archivo = Bun.file(new URL(`.${url.pathname}`, carpetaCompilada))
    if (url.pathname !== '/' && (await archivo.exists())) return new Response(archivo)
    // Rutas del SPA: cualquier otra cosa devuelve la app.
    return new Response(Bun.file(new URL('index.html', carpetaCompilada)), { headers: { 'content-type': 'text/html; charset=utf-8' } })
  },
})

process.on('SIGTERM', () => {
  servidor.stop()
  process.exit(0)
})

console.log(`frontend en http://localhost:${puerto}`)
