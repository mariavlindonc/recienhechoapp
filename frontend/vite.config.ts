import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// En desarrollo, /api llega al api-gateway y se le saca el prefijo (ADR-001).
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: process.env.API_GATEWAY_URL ?? 'http://localhost:8080',
        changeOrigin: true,
        rewrite: ruta => ruta.replace(/^\/api/, ''),
        // El WebSocket del seguimiento en vivo (ADR-018). Sin Origin, porque pedidos rechaza uno
        // distinto de su host. Es solo para desarrollo: en producción lo controla servidor.ts.
        ws: true,
        configure: proxy => proxy.on('proxyReqWs', solicitud => solicitud.removeHeader('origin')),
      },
    },
  },
})
