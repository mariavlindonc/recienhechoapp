import { afterEach, expect, test } from 'bun:test'
import { aPedido, aProducto, buscarProductos, crearPedido, ErrorDeApi, esperaParaReconectar, ingresar, listarEspeciales, listarMisPedidos, rechazarEspecial, responderCotizacion, seguirPedidos, TRANSICIONES } from './api.ts'

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
  id: 'p1', nombre: 'Budín de limón', descripcion: 'Con semillas de amapola y limones caramelizados', categoriaId: 'c1', categoria: 'Cítricos',
  precioCentavos: 850000, anticipacionMinimaDias: 2, aptoCeliacos: false, disponible: true, imagenUrl: '/imagenes/p1.png',
}

test('traduce el producto de productos a la forma de las pantallas', () => {
  const producto = aProducto(productoDeApi)
  expect(producto.anticipacionDias).toBe(2)
  expect(producto.categoria).toBe('Cítricos')
  expect(producto.imagenUrl).toBe('/api/imagenes/p1.png')
})

test('un pedido con varios productos suma las cantidades y junta los nombres', () => {
  const pedido = aPedido({
    id: 'pd1', fecha: '2026-11-02', precioCentavos: 100, tipo: 'programado', estado: 'confirmado',
    items: [{ productoId: 'p1', nombreProducto: 'Budín de limón', cantidad: 1 }, { productoId: 'p2', nombreProducto: 'Budín de zanahoria', cantidad: 2 }],
  })
  expect(pedido.cantidad).toBe(3)
  expect(pedido.nombreProducto).toBe('Budín de limón, Budín de zanahoria')
})

test('la búsqueda manda los filtros como los espera productos (ADR-008)', async () => {
  const pedidos = responder(200, { productos: [productoDeApi], pagina: 1, porPagina: 20, total: 1 })

  const pagina = await buscarProductos({ texto: 'limón', categoria: 'Cítricos', precioMaximo: 900000, aptoCeliacos: true, orden: 'precio-asc' })

  const url = new URL(pedidos[0]!.url, 'http://local')
  expect(url.pathname).toBe('/api/productos/busqueda')
  expect(Object.fromEntries(url.searchParams)).toEqual({ q: 'limón', pagina: '1', disponible: 'true', categoria: 'Cítricos', precioMax: '900000', aptoCeliacos: 'true', orden: 'precio_asc' })
  expect(pagina.items[0]!.nombre).toBe('Budín de limón')
})

test('con sesión manda el token y el pedido lleva Idempotency-Key', async () => {
  responder(200, { token: 'token-1', usuario: { id: 'u1', email: 'a@a.com', nombre: 'Ana', rol: 'comprador' } })
  await ingresar('a@a.com', 'clave-segura')
  const pedidos = responder(201, { id: 'pd1', items: [], fecha: '2026-11-02', precioCentavos: 0, tipo: 'inmediato', estado: 'pendiente_confirmacion' })

  await crearPedido({ productoId: 'p1', cantidad: 1, clave: 'clave-1' })

  const headers = pedidos[0]!.opciones.headers as Record<string, string>
  expect(headers.Authorization).toBe('Bearer token-1')
  expect(headers['Idempotency-Key']).toBeTruthy()
  expect(JSON.parse(pedidos[0]!.opciones.body as string)).toEqual({ tipo: 'inmediato', items: [{ productoId: 'p1', cantidad: 1 }] })
})

test('un error del servicio llega con el mensaje del contrato', async () => {
  responder(409, { codigo: 'sin_cupo', mensaje: 'No hay cupo para ese día.' })

  const error = await crearPedido({ productoId: 'p1', cantidad: 1, fecha: '2026-11-02', clave: 'clave-1' }).catch(motivo => motivo)

  expect(error).toBeInstanceOf(ErrorDeApi)
  expect(error.message).toBe('No hay cupo para ese día.')
  expect(error.codigo).toBe('sin_cupo')
})

test('los especiales sin aceptar van aparte; aceptados son un pedido más (ADR-017)', async () => {
  const item = { productoId: '', nombreProducto: 'Budín de zanahoria sin gluten con etiqueta para regalar', cantidad: 1 }
  responder(200, [
    { id: 'e1', items: [item], fecha: '2026-12-01', precioCentavos: 2500000, tipo: 'especial', estado: 'cotizado' },
    { id: 'e2', items: [item], fecha: '2026-12-02', precioCentavos: 2500000, tipo: 'especial', estado: 'confirmado' },
  ])

  const especiales = await listarEspeciales()
  const pedidos = await listarMisPedidos()

  expect(especiales.map(especial => [especial.id, especial.descripcion, especial.precioCentavos])).toEqual([['e1', 'Budín de zanahoria sin gluten con etiqueta para regalar', 2500000]])
  expect(pedidos.map(pedido => pedido.id)).toEqual(['e2'])
})

test('R8: la cotización vencida llega como error de pedidos', async () => {
  const pedidos = responder(409, { codigo: 'cotizacion_vencida', mensaje: 'La cotización venció: pasaron más de 48 horas.' })

  const error = await responderCotizacion('e1', true).catch(motivo => motivo)

  expect(pedidos[0]!.url).toBe('/api/pedidos/especiales/e1/aceptacion')
  expect(error.codigo).toBe('cotizacion_vencida')
})

test('crearPedido manda la clave del intento, así un reintento no duplica el pedido', async () => {
  const pedidos = responder(201, { id: 'pd1', items: [], fecha: '2026-11-02', precioCentavos: 0, tipo: 'inmediato', estado: 'pendiente_confirmacion' })

  await crearPedido({ productoId: 'p1', cantidad: 1, clave: 'clave-del-intento' })
  await crearPedido({ productoId: 'p1', cantidad: 1, clave: 'clave-del-intento' })

  const claves = pedidos.map(pedido => (pedido.opciones.headers as Record<string, string>)['Idempotency-Key'])
  expect(claves).toEqual(['clave-del-intento', 'clave-del-intento'])
})

test('el vendedor rechaza una solicitud con un mensaje para el comprador (ADR-017)', async () => {
  const pedidos = responder(200, {
    id: 'e1', items: [{ productoId: '', nombreProducto: 'Torta de seis pisos', cantidad: 1 }], fecha: '2026-12-01', precioCentavos: 0,
    tipo: 'especial', estado: 'cancelado', motivoCancelacion: 'Rechazado por la pastelería.', mensajeVendedor: 'No llegamos para esa fecha.',
  })

  const rechazado = await rechazarEspecial('e1', 'No llegamos para esa fecha.')

  expect(pedidos[0]!.url).toBe('/api/pedidos/especiales/e1/declinacion')
  expect(pedidos[0]!.opciones.method).toBe('POST')
  expect(JSON.parse(pedidos[0]!.opciones.body as string)).toEqual({ mensaje: 'No llegamos para esa fecha.' })
  expect(rechazado.estado).toBe('cancelado')
  expect(rechazado.mensajeVendedor).toBe('No llegamos para esa fecha.')
})

test('el comprador ve el mensaje del vendedor en sus pedidos', async () => {
  responder(200, [{
    id: 'e1', items: [{ productoId: '', nombreProducto: 'Torta de seis pisos', cantidad: 1 }], fecha: '2026-12-01', precioCentavos: 0,
    tipo: 'especial', estado: 'cancelado', motivoCancelacion: 'Rechazado por la pastelería.', mensajeVendedor: 'No llegamos para esa fecha.',
  }])

  const [pedido] = await listarMisPedidos()

  expect(pedido!.mensajeVendedor).toBe('No llegamos para esa fecha.')
  expect(pedido!.motivoCancelacion).toBe('Rechazado por la pastelería.')
})

// R6: un envío pasa por en_camino; un retiro va de listo a entregado. El vendedor ve los dos botones.
test('desde listo se puede mandar en camino o entregar directo', () => {
  expect(TRANSICIONES.listo).toEqual(['en_camino', 'entregado'])
  expect(TRANSICIONES.en_camino).toEqual(['entregado'])
})

// WebSocket falso: guarda las conexiones abiertas para que el test maneje cada una.
class SocketFalso {
  static abiertos: SocketFalso[] = []
  onopen?: () => void
  onmessage?: (evento: { data: string }) => void
  onclose?: () => void
  cerrado = false
  constructor(readonly url: string) {
    SocketFalso.abiertos.push(this)
  }
  close() {
    this.cerrado = true
    this.onclose?.()
  }
}

async function conSesion() {
  responder(200, { token: 'token/1', usuario: { id: 'u1', email: 'a@a.com', nombre: 'Ana', rol: 'comprador' } })
  await ingresar('a@a.com', 'clave-segura')
}

function prepararSockets() {
  SocketFalso.abiertos = []
  globalThis.WebSocket = SocketFalso as unknown as typeof WebSocket
  globalThis.location = { protocol: 'http:', host: 'localhost:5173' } as Location
}

test('B2: se conecta con el token, se pone al día y actualiza con cada cambio (ADR-018)', async () => {
  await conSesion()
  prepararSockets()
  let actualizaciones = 0

  const dejar = seguirPedidos(() => actualizaciones++, () => 0)
  const socket = SocketFalso.abiertos[0]!
  expect(socket.url).toBe('ws://localhost:5173/api/pedidos/tiempo-real?token=token%2F1')

  socket.onopen!()
  expect(actualizaciones).toBe(1)
  socket.onmessage!({ data: JSON.stringify({ tipo: 'estado_cambiado', idPedido: 'pd1', estadoNuevo: 'listo' }) })
  expect(actualizaciones).toBe(2)
  // Los tipos nuevos, como la ubicación del delivery, se ignoran hasta que la pantalla los use.
  socket.onmessage!({ data: JSON.stringify({ tipo: 'ubicacion', idPedido: 'pd1' }) })
  expect(actualizaciones).toBe(2)
  dejar()
})

test('B2: si se corta se reconecta sola y, al volver, se pone al día', async () => {
  await conSesion()
  prepararSockets()
  let actualizaciones = 0

  const dejar = seguirPedidos(() => actualizaciones++, () => 0)
  SocketFalso.abiertos[0]!.onclose!()
  await Bun.sleep(5)

  expect(SocketFalso.abiertos).toHaveLength(2)
  SocketFalso.abiertos[1]!.onopen!()
  expect(actualizaciones).toBe(1)
  dejar()
})

test('al salir de la pantalla se cierra y no se reconecta', async () => {
  await conSesion()
  prepararSockets()

  const dejar = seguirPedidos(() => {}, () => 0)
  dejar()
  await Bun.sleep(5)

  expect(SocketFalso.abiertos[0]!.cerrado).toBe(true)
  expect(SocketFalso.abiertos).toHaveLength(1)
})

test('sin sesión de comprador no se conecta', () => {
  prepararSockets()

  seguirPedidos(() => {}, () => 0)()

  expect(SocketFalso.abiertos).toHaveLength(0)
})

test('las esperas para reconectarse crecen hasta 30 s', () => {
  expect([0, 1, 2, 3, 4, 5, 6, 20].map(esperaParaReconectar)).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000])
})
