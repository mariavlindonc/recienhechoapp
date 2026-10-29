import { afterEach, expect, test } from 'bun:test'
import { aPedido, aProducto, buscarProductos, cotizarEspecial, crearPedido, ErrorDeApi, ingresar, responderCotizacion, solicitarPedidoEspecial } from './api.ts'

// sessionStorage no existe en bun test: alcanza con un mapa.
const guardado = new Map<string, string>()
globalThis.sessionStorage = {
  getItem: (clave: string) => guardado.get(clave) ?? null,
  setItem: (clave: string, valor: string) => void guardado.set(clave, valor),
  removeItem: (clave: string) => void guardado.delete(clave),
} as Storage

const fetchOriginal = globalThis.fetch
afterEach(() => {
  globalThis.fetch = fetchOriginal
  guardado.clear()
})

// responder hace que fetch devuelva siempre lo mismo y anota cada solicitud.
function responder(estado: number, cuerpo: unknown) {
  const pedidos: { url: string; opciones: RequestInit }[] = []
  globalThis.fetch = (async (url: string, opciones: RequestInit) => {
    pedidos.push({ url, opciones })
    return new Response(JSON.stringify(cuerpo), { status: estado })
  }) as typeof fetch
  return pedidos
}

const productoDeApi = {
  id: 'p1', nombre: 'Torta', descripcion: 'De limón', categoriaId: 'c1', categoria: 'Tortas',
  precioCentavos: 850000, anticipacionMinimaDias: 2, aptoCeliacos: false, disponible: true, imagenUrl: '/imagenes/p1.png',
}

test('traduce el producto de productos a la forma de las pantallas', () => {
  const producto = aProducto(productoDeApi)
  expect(producto.anticipacionDias).toBe(2)
  expect(producto.categoria).toBe('Tortas')
  expect(producto.imagenUrl).toBe('/api/imagenes/p1.png')
})

test('un pedido con varios productos suma las cantidades y junta los nombres', () => {
  const pedido = aPedido({
    id: 'pd1', fecha: '2026-11-02', precioCentavos: 100, tipo: 'programado', estado: 'confirmado',
    items: [{ productoId: 'p1', nombreProducto: 'Torta', cantidad: 1 }, { productoId: 'p2', nombreProducto: 'Budín', cantidad: 2 }],
  })
  expect(pedido.cantidad).toBe(3)
  expect(pedido.nombreProducto).toBe('Torta, Budín')
})

test('la búsqueda manda los filtros como los espera productos (ADR-008)', async () => {
  const pedidos = responder(200, { productos: [productoDeApi], pagina: 1, porPagina: 20, total: 1 })

  const pagina = await buscarProductos({ texto: 'torta', categoria: 'Tortas', precioMaximo: 900000, aptoCeliacos: true, orden: 'precio-asc' })

  const url = new URL(pedidos[0]!.url, 'http://local')
  expect(url.pathname).toBe('/api/productos/busqueda')
  expect(Object.fromEntries(url.searchParams)).toEqual({ q: 'torta', pagina: '1', disponible: 'true', categoria: 'Tortas', precioMax: '900000', aptoCeliacos: 'true', orden: 'precio_asc' })
  expect(pagina.items[0]!.nombre).toBe('Torta')
})

test('con sesión manda el token y el pedido lleva Idempotency-Key', async () => {
  responder(200, { token: 'token-1', usuario: { id: 'u1', email: 'a@a.com', nombre: 'Ana', rol: 'comprador' } })
  await ingresar('a@a.com', 'clave-segura')
  const pedidos = responder(201, { id: 'pd1', items: [], fecha: '2026-11-02', precioCentavos: 0, tipo: 'inmediato', estado: 'pendiente_confirmacion' })

  await crearPedido({ productoId: 'p1', cantidad: 1 })

  const headers = pedidos[0]!.opciones.headers as Record<string, string>
  expect(headers.Authorization).toBe('Bearer token-1')
  expect(headers['Idempotency-Key']).toBeTruthy()
  expect(JSON.parse(pedidos[0]!.opciones.body as string)).toEqual({ tipo: 'inmediato', items: [{ productoId: 'p1', cantidad: 1 }] })
})

test('un error del servicio llega con el mensaje del contrato', async () => {
  responder(409, { codigo: 'sin_cupo', mensaje: 'No hay cupo para ese día.' })

  const error = await crearPedido({ productoId: 'p1', cantidad: 1, fecha: '2026-11-02' }).catch(motivo => motivo)

  expect(error).toBeInstanceOf(ErrorDeApi)
  expect(error.message).toBe('No hay cupo para ese día.')
  expect(error.codigo).toBe('sin_cupo')
})

test('R8: rechaza una cotización vencida después de 48 h', async () => {
  const especial = await solicitarPedidoEspecial({ descripcion: 'Torta temática', fecha: '2026-12-01' })
  await cotizarEspecial(especial.id, 2500000)
  const en49Horas = new Date(Date.now() + 49 * 36e5)
  expect(() => responderCotizacion(especial.id, true, en49Horas)).toThrow('venció')
})
