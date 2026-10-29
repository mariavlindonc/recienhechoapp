// Único punto de acceso a datos del frontend (ADR-014). Todo va al api-gateway por /api,
// con el token de la sesión (ADR-016). Acá se traducen las respuestas de los servicios a los
// tipos de las pantallas, así las pantallas no conocen la forma de cada servicio.
import { hoyIso, sumarDias } from './formato.ts'
import { borrarSesion, guardarSesion, leerSesion } from './sesion.ts'
import type {
  Categoria,
  DatosProducto,
  DiaCapacidad,
  EstadoPedido,
  FiltrosCatalogo,
  Ingrediente,
  PaginaDeProductos,
  Pedido,
  PedidoEspecial,
  Producto,
  Usuario,
} from './tipos.ts'

// Días que miran las pantallas del vendedor: una semana atrás (lo atrasado) y un mes adelante.
const DIAS_ATRAS = 7
const DIAS_ADELANTE = 30

// Estados de docs/ARCHITECTURE.md, "Estados de un pedido".
export const TRANSICIONES: Record<EstadoPedido, EstadoPedido[]> = {
  pendiente_confirmacion: ['confirmado', 'cancelado'],
  confirmado: ['en_produccion', 'cancelado'],
  en_produccion: ['listo'],
  listo: ['entregado'],
  entregado: [],
  cancelado: [],
}

// ErrorDeApi lleva el código del contrato ({codigo, mensaje}) y el estado HTTP.
export class ErrorDeApi extends Error {
  constructor(
    mensaje: string,
    readonly estado: number,
    readonly codigo: string,
  ) {
    super(mensaje)
  }
}

async function pedir<Tipo>(ruta: string, { metodo = 'GET', cuerpo, headers = {} }: { metodo?: string; cuerpo?: unknown; headers?: Record<string, string> } = {}): Promise<Tipo> {
  const sesion = leerSesion()
  const respuesta = await fetch(`/api${ruta}`, {
    method: metodo,
    headers: {
      ...(cuerpo === undefined ? {} : { 'Content-Type': 'application/json' }),
      ...(sesion ? { Authorization: `Bearer ${sesion.token}` } : {}),
      ...headers,
    },
    body: cuerpo === undefined ? undefined : JSON.stringify(cuerpo),
  })
  if (respuesta.ok) return respuesta.status === 204 ? (undefined as Tipo) : ((await respuesta.json()) as Tipo)

  const error = (await respuesta.json().catch(() => ({}))) as { codigo?: string; mensaje?: string }
  // Sesión vencida o inválida: se borra y se manda a ingresar. El login fallido no cuenta.
  if (respuesta.status === 401 && ruta !== '/usuarios/sesion') {
    borrarSesion()
    window.location.assign('/ingresar')
  }
  throw new ErrorDeApi(error.mensaje ?? `Error ${respuesta.status}`, respuesta.status, error.codigo ?? '')
}

// Sesión (C1)
export async function ingresar(email: string, clave: string) {
  const respuesta = await pedir<{ token: string; usuario: Usuario }>('/usuarios/sesion', { metodo: 'POST', cuerpo: { email, clave } })
  guardarSesion({ token: respuesta.token, usuario: respuesta.usuario })
  return respuesta.usuario
}

export async function registrarse(datos: { nombre: string; email: string; clave: string }) {
  await pedir<Usuario>('/usuarios', { metodo: 'POST', cuerpo: datos })
  return ingresar(datos.email, datos.clave)
}

// Cerrar sesión es borrar el token: sigue valiendo hasta que vence (ADR-016).
export function cerrarSesion() {
  borrarSesion()
}

// Catálogo (C2, C3)
type ProductoDeApi = {
  id: string
  nombre: string
  descripcion: string
  categoriaId: string
  categoria: string
  precioCentavos: number
  anticipacionMinimaDias: number
  aptoCeliacos: boolean
  disponible: boolean
  imagenUrl: string
}
type PaginaDeApi = { productos: ProductoDeApi[]; pagina: number; porPagina: number; total: number }

export function aProducto(producto: ProductoDeApi): Producto {
  return {
    id: producto.id,
    nombre: producto.nombre,
    descripcion: producto.descripcion,
    categoria: producto.categoria,
    precioCentavos: producto.precioCentavos,
    anticipacionDias: producto.anticipacionMinimaDias,
    aptoCeliacos: producto.aptoCeliacos,
    disponible: producto.disponible,
    // La foto la sirve productos en /imagenes, que el gateway enruta.
    imagenUrl: producto.imagenUrl ? `/api${producto.imagenUrl}` : '',
  }
}

const ORDENES: Record<NonNullable<FiltrosCatalogo['orden']>, string> = { relevancia: '', 'precio-asc': 'precio_asc', 'precio-desc': 'precio_desc' }

export async function buscarProductos({ texto = '', categoria = '', precioMaximo, aptoCeliacos = false, orden = 'relevancia', pagina = 1 }: FiltrosCatalogo = {}): Promise<PaginaDeProductos> {
  const parametros = new URLSearchParams({ q: texto.trim(), pagina: String(pagina), disponible: 'true' })
  if (categoria) parametros.set('categoria', categoria)
  if (precioMaximo !== undefined) parametros.set('precioMax', String(precioMaximo))
  if (aptoCeliacos) parametros.set('aptoCeliacos', 'true')
  if (ORDENES[orden]) parametros.set('orden', ORDENES[orden])
  const respuesta = await pedir<PaginaDeApi>(`/productos/busqueda?${parametros}`)
  return { items: respuesta.productos.map(aProducto), total: respuesta.total, pagina: respuesta.pagina, porPagina: respuesta.porPagina }
}

export async function obtenerProducto(id: string): Promise<Producto | null> {
  try {
    return aProducto(await pedir<ProductoDeApi>(`/productos/${encodeURIComponent(id)}`))
  } catch (error) {
    if (error instanceof ErrorDeApi && error.estado === 404) return null
    throw error
  }
}

export function listarCategorias() {
  return pedir<Categoria[]>('/categorias')
}

// El vendedor ve todos sus productos, también los no disponibles: se piden todas las páginas.
export async function listarProductosVendedor() {
  const productos: Producto[] = []
  for (let pagina = 1; ; pagina++) {
    const respuesta = await pedir<PaginaDeApi>(`/productos?incluirNoDisponibles=true&pagina=${pagina}`)
    productos.push(...respuesta.productos.map(aProducto))
    if (pagina * respuesta.porPagina >= respuesta.total) return productos
  }
}

// La pantalla carga la categoría por nombre: si no existe, se crea.
async function idDeCategoria(nombre: string) {
  const buscada = nombre.trim().toLowerCase()
  const existente = (await listarCategorias()).find(categoria => categoria.nombre.toLowerCase() === buscada)
  return existente?.id ?? (await pedir<Categoria>('/categorias', { metodo: 'POST', cuerpo: { nombre: nombre.trim() } })).id
}

export async function guardarProducto(datos: DatosProducto) {
  if (!Number.isInteger(datos.precioCentavos)) throw new Error('el precio debe ser un entero en centavos')
  const cuerpo = {
    nombre: datos.nombre,
    descripcion: datos.descripcion,
    categoriaId: await idDeCategoria(datos.categoria),
    precioCentavos: datos.precioCentavos,
    anticipacionMinimaDias: datos.anticipacionDias,
    aptoCeliacos: datos.aptoCeliacos,
    disponible: datos.disponible ?? true,
  }
  const guardado = datos.id
    ? await pedir<ProductoDeApi>(`/productos/${encodeURIComponent(datos.id)}`, { metodo: 'PUT', cuerpo })
    : await pedir<ProductoDeApi>('/productos', { metodo: 'POST', cuerpo })
  return aProducto(guardado)
}

export async function borrarProducto(id: string) {
  await pedir(`/productos/${encodeURIComponent(id)}`, { metodo: 'DELETE' })
  return true
}

export async function obtenerReceta(productoId: string) {
  return (await pedir<{ receta: Ingrediente[] }>(`/productos/${encodeURIComponent(productoId)}/receta`)).receta
}

export async function guardarReceta(productoId: string, ingredientes: Ingrediente[]) {
  await pedir(`/productos/${encodeURIComponent(productoId)}/receta`, { metodo: 'PUT', cuerpo: { receta: ingredientes } })
  return ingredientes
}

// Pedidos (C4, C5, C7, C8)
type PedidoDeApi = {
  id: string
  items: { productoId: string; nombreProducto: string; cantidad: number }[]
  fecha: string
  franja?: string
  precioCentavos: number
  tipo: 'programado' | 'inmediato' | 'especial'
  estado: EstadoPedido
}

// Las pantallas muestran un producto por pedido; si hay varios, se juntan los nombres.
export function aPedido(pedido: PedidoDeApi): Pedido {
  return {
    id: pedido.id,
    productoId: pedido.items[0]?.productoId ?? '',
    nombreProducto: pedido.items.map(item => item.nombreProducto).join(', '),
    cantidad: pedido.items.reduce((total, item) => total + item.cantidad, 0),
    fecha: pedido.fecha,
    franja: pedido.franja,
    precioCentavos: pedido.precioCentavos,
    tipo: pedido.tipo,
    estado: pedido.estado,
  }
}

// Sin fecha es un pedido inmediato: pedidos busca el primer día con cupo (C4).
export async function crearPedido({ productoId, cantidad, fecha, franja }: { productoId: string; cantidad: number; fecha?: string; franja?: string }) {
  const cuerpo = fecha ? { tipo: 'programado', items: [{ productoId, cantidad }], fecha, franja } : { tipo: 'inmediato', items: [{ productoId, cantidad }] }
  // Una clave por intento: si la red corta, reintentar con la misma no duplica el pedido (ADR-003).
  const pedido = await pedir<PedidoDeApi>('/pedidos', { metodo: 'POST', cuerpo, headers: { 'Idempotency-Key': crypto.randomUUID() } })
  return aPedido(pedido)
}

export async function listarMisPedidos() {
  return (await pedir<PedidoDeApi[]>('/pedidos')).filter(pedido => !esEspecialAbierto(pedido)).map(aPedido)
}

export async function cancelarPedido(id: string) {
  return aPedido(await pedir<PedidoDeApi>(`/pedidos/${encodeURIComponent(id)}/cancelacion`, { metodo: 'POST' }))
}

// Vendedor: estados (VE6)
export async function avanzarEstado(id: string, nuevoEstado: EstadoPedido) {
  return aPedido(await pedir<PedidoDeApi>(`/pedidos/${encodeURIComponent(id)}/estado`, { metodo: 'PUT', cuerpo: { estado: nuevoEstado } }))
}

// pedidos lista por día: se piden solo los días que tienen algo reservado.
export async function listarPedidosVendedor() {
  const dias = await listarCapacidad()
  const conReservas = Object.keys(dias).filter(fecha => dias[fecha]!.reservados > 0)
  const porDia = await Promise.all(conReservas.map(fecha => pedir<PedidoDeApi[]>(`/pedidos?fecha=${fecha}`)))
  return porDia.flat().map(aPedido)
}

// Capacidad y calendario (VE3, VE4)
export function listarCapacidad() {
  const hoy = hoyIso()
  return pedir<Record<string, DiaCapacidad>>(`/capacidad?desde=${sumarDias(hoy, -DIAS_ATRAS)}&hasta=${sumarDias(hoy, DIAS_ADELANTE)}`)
}

export function guardarCapacidad(fecha: string, nuevaCapacidad: number) {
  return pedir<DiaCapacidad>(`/capacidad/${fecha}`, { metodo: 'PUT', cuerpo: { capacidad: nuevaCapacidad } })
}

// Pedidos especiales (C6, VE5, R8; ADR-017). pedidos controla el vencimiento a las 48 h.
const ESTADOS_ESPECIALES = ['solicitado', 'cotizado', 'rechazado', 'vencido']

// Mientras espera la cotización o la respuesta, un especial se muestra aparte; aceptado, es un pedido más.
const esEspecialAbierto = (pedido: PedidoDeApi) => pedido.tipo === 'especial' && ESTADOS_ESPECIALES.includes(pedido.estado)

export function aEspecial(pedido: PedidoDeApi): PedidoEspecial {
  return {
    id: pedido.id,
    descripcion: pedido.items[0]?.nombreProducto ?? '',
    fecha: pedido.fecha,
    estado: pedido.estado as unknown as PedidoEspecial['estado'],
    precioCentavos: pedido.precioCentavos || null,
  }
}

export async function solicitarPedidoEspecial({ descripcion, fecha }: { descripcion: string; fecha: string }) {
  const pedido = await pedir<PedidoDeApi>('/pedidos/especiales', { metodo: 'POST', cuerpo: { descripcion, fecha }, headers: { 'Idempotency-Key': crypto.randomUUID() } })
  return aEspecial(pedido)
}

// El vendedor ve los que tiene que cotizar o esperan respuesta; el comprador, los suyos.
export async function listarEspeciales() {
  if (leerSesion()?.usuario.rol === 'vendedor') return (await pedir<PedidoDeApi[]>('/pedidos/especiales')).map(aEspecial)
  return (await pedir<PedidoDeApi[]>('/pedidos')).filter(esEspecialAbierto).map(aEspecial)
}

export async function cotizarEspecial(id: string, precioCentavos: number) {
  return aEspecial(await pedir<PedidoDeApi>(`/pedidos/especiales/${encodeURIComponent(id)}/cotizacion`, { metodo: 'PUT', cuerpo: { precioCentavos } }))
}

// Al aceptar reserva el cupo del día; vencidas las 48 h, pedidos responde 409 cotizacion_vencida (R8).
export async function responderCotizacion(id: string, acepta: boolean) {
  const accion = acepta ? 'aceptacion' : 'rechazo'
  return aPedido(await pedir<PedidoDeApi>(`/pedidos/especiales/${encodeURIComponent(id)}/${accion}`, { metodo: 'POST' }))
}
