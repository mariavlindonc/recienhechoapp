// Único punto de acceso a datos del frontend. Hoy resuelve contra datos de ejemplo en memoria;
// cuando existan los servicios, cada función pasa a fetch('/api/...') sin cambiar las pantallas.
// shortcut: reglas validadas en el navegador sobre datos de ejemplo, pasan a pedidos y productos cuando existan.
import {
  capacidadIniciales,
  especialesIniciales,
  pedidosIniciales,
  productosIniciales,
  recetasIniciales,
} from './datosDeEjemplo.js'
import { hoyIso, sumarDias } from './formato.js'

const TAMANIO_PAGINA = 20
const HORAS_VENCIMIENTO_COTIZACION = 48

// Estados de docs/ARCHITECTURE.md, "Estados de un pedido".
export const TRANSICIONES = {
  pendiente_confirmacion: ['confirmado', 'cancelado'],
  confirmado: ['en_produccion', 'cancelado'],
  en_produccion: ['listo'],
  listo: ['entregado'],
  entregado: [],
  cancelado: [],
}

const productos = structuredClone(productosIniciales)
const recetas = structuredClone(recetasIniciales)
const capacidad = structuredClone(capacidadIniciales)
const pedidos = structuredClone(pedidosIniciales)
const especiales = structuredClone(especialesIniciales)

const esperar = valor => new Promise(resolver => setTimeout(() => resolver(structuredClone(valor)), 80))

// Catálogo (C2, C3)
export function buscarProductos({ texto = '', categoria = '', precioMaximo, aptoCeliacos = false, orden = 'nombre', pagina = 1 } = {}) {
  const textoBuscado = texto.trim().toLowerCase()
  const filtrados = productos
    .filter(producto => producto.disponible)
    .filter(producto => !textoBuscado || producto.nombre.toLowerCase().includes(textoBuscado))
    .filter(producto => !categoria || producto.categoria === categoria)
    .filter(producto => precioMaximo === undefined || producto.precioCentavos <= precioMaximo)
    .filter(producto => !aptoCeliacos || producto.aptoCeliacos)
    .sort((primero, segundo) => {
      if (orden === 'precio-asc') return primero.precioCentavos - segundo.precioCentavos
      if (orden === 'precio-desc') return segundo.precioCentavos - primero.precioCentavos
      return primero.nombre.localeCompare(segundo.nombre, 'es')
    })
  const inicio = (pagina - 1) * TAMANIO_PAGINA
  return esperar({ items: filtrados.slice(inicio, inicio + TAMANIO_PAGINA), total: filtrados.length, pagina })
}

export function obtenerProducto(id) {
  const producto = productos.find(item => item.id === id)
  return esperar(producto ?? null)
}

export function listarProductosVendedor() {
  return esperar(productos)
}

export function guardarProducto(datos) {
  if (!Number.isInteger(datos.precioCentavos)) throw new Error('el precio debe ser un entero en centavos')
  if (datos.id) {
    const indice = productos.findIndex(item => item.id === datos.id)
    productos[indice] = { ...productos[indice], ...datos }
    return esperar(productos[indice])
  }
  const nuevo = { ...datos, id: `p${Date.now()}`, disponible: true }
  productos.push(nuevo)
  return esperar(nuevo)
}

export function borrarProducto(id) {
  const indice = productos.findIndex(item => item.id === id)
  if (indice >= 0) productos.splice(indice, 1)
  return esperar(true)
}

export function obtenerReceta(productoId) {
  return esperar(recetas[productoId] ?? [])
}

export function guardarReceta(productoId, ingredientes) {
  recetas[productoId] = ingredientes
  return esperar(ingredientes)
}

// Pedidos (C4, C5, C7, C8)
export function crearPedido({ productoId, cantidad, fecha, franja }) {
  const producto = productos.find(item => item.id === productoId)
  if (!producto) throw new Error('el producto no existe')

  // R2: anticipación mínima del producto.
  const primeraFechaPosible = sumarDias(hoyIso(), producto.anticipacionDias)
  if (fecha < primeraFechaPosible) {
    throw new Error(`pedido con muy poca anticipación. La primera fecha posible es ${primeraFechaPosible}`)
  }

  // R1: el cupo se reserva solo si alcanza.
  const dia = capacidad[fecha] ?? { capacidad: 0, reservados: 0 }
  if (dia.reservados + cantidad > dia.capacidad) {
    throw new Error('sin cupo para ese día')
  }
  dia.reservados += cantidad
  capacidad[fecha] = dia

  // R4: el precio queda fijado al crear el pedido.
  const pedido = {
    id: `pd${Date.now()}`,
    productoId,
    nombreProducto: producto.nombre,
    cantidad,
    fecha,
    franja,
    precioCentavos: producto.precioCentavos * cantidad,
    tipo: franja ? 'programado' : 'inmediato',
    estado: 'pendiente_confirmacion',
  }
  pedidos.push(pedido)
  return esperar(pedido)
}

export function listarMisPedidos() {
  return esperar(pedidos)
}

// R6 y R7: solo se cancela antes de producción; cancelar libera el cupo.
export function cancelarPedido(id) {
  const pedido = pedidos.find(item => item.id === id)
  if (!pedido) throw new Error('el pedido no existe')
  if (!['pendiente_confirmacion', 'confirmado'].includes(pedido.estado)) {
    throw new Error('el pedido ya entró en producción y no se puede cancelar')
  }
  pedido.estado = 'cancelado'
  const dia = capacidad[pedido.fecha]
  if (dia) dia.reservados -= pedido.cantidad
  return esperar(pedido)
}

// Vendedor: estados (VE6)
export function avanzarEstado(id, nuevoEstado) {
  const pedido = pedidos.find(item => item.id === id)
  if (!pedido) throw new Error('el pedido no existe')
  if (!TRANSICIONES[pedido.estado].includes(nuevoEstado)) {
    throw new Error(`no se puede pasar de ${pedido.estado} a ${nuevoEstado}`)
  }
  pedido.estado = nuevoEstado
  return esperar(pedido)
}

export function listarPedidosVendedor() {
  return esperar(pedidos)
}

// Capacidad y calendario (VE3, VE4)
export function listarCapacidad() {
  return esperar(capacidad)
}

// VE3: no se puede bajar la capacidad por debajo de lo reservado.
export function guardarCapacidad(fecha, nuevaCapacidad) {
  const dia = capacidad[fecha] ?? { capacidad: 0, reservados: 0 }
  if (nuevaCapacidad < dia.reservados) {
    throw new Error(`la capacidad no puede ser menor a los ${dia.reservados} reservados`)
  }
  capacidad[fecha] = { ...dia, capacidad: nuevaCapacidad }
  return esperar(capacidad[fecha])
}

// Pedidos especiales (C6, VE5, R8)
export function solicitarPedidoEspecial({ descripcion, fecha }) {
  const especial = { id: `es${Date.now()}`, descripcion, fecha, estado: 'solicitado', precioCentavos: null, cotizadoEn: null }
  especiales.push(especial)
  return esperar(especial)
}

export function listarEspeciales() {
  return esperar(especiales)
}

export function cotizarEspecial(id, precioCentavos) {
  const especial = especiales.find(item => item.id === id)
  if (!especial) throw new Error('el pedido especial no existe')
  Object.assign(especial, { estado: 'cotizado', precioCentavos, cotizadoEn: new Date().toISOString() })
  return esperar(especial)
}

// R8: una cotización vence a las 48 h si el comprador no la acepta.
export function responderCotizacion(id, acepta, ahora = new Date()) {
  const especial = especiales.find(item => item.id === id)
  if (!especial) throw new Error('el pedido especial no existe')
  const horasDesdeCotizacion = (ahora - new Date(especial.cotizadoEn)) / 36e5
  if (horasDesdeCotizacion > HORAS_VENCIMIENTO_COTIZACION) {
    especial.estado = 'vencido'
    throw new Error('la cotización venció')
  }
  especial.estado = acepta ? 'aceptado' : 'rechazado'
  return esperar(especial)
}
