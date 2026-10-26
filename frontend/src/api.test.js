import { expect, test } from 'bun:test'
import { crearPedido, responderCotizacion, cotizarEspecial, solicitarPedidoEspecial, guardarCapacidad } from './api.js'
import { hoyIso } from './formato.js'

test('R2: no acepta pedidos con menos anticipación que la del producto', () => {
  // Budin 1 pide 1 día de anticipación: para hoy no alcanza.
  expect(() => crearPedido({ productoId: 'p1', cantidad: 1, fecha: hoyIso() })).toThrow('primera fecha posible')
})

test('R1: no acepta un pedido que supera el cupo del día', () => {
  // 2026-10-30 tiene 10 de capacidad y 9 reservados. Fecha fija: el test vence si pasa el día.
  expect(() => crearPedido({ productoId: 'p1', cantidad: 2, fecha: '2026-10-30' })).toThrow('sin cupo')
})

test('VE3: no baja la capacidad por debajo de lo reservado', () => {
  expect(() => guardarCapacidad('2026-10-30', 5)).toThrow('reservados')
})

test('R8: rechaza una cotización vencida después de 48 h', async () => {
  const especial = await solicitarPedidoEspecial({ descripcion: 'Torta temática', fecha: '2026-12-01' })
  await cotizarEspecial(especial.id, 2500000)
  const en49Horas = new Date(Date.now() + 49 * 36e5)
  expect(() => responderCotizacion(especial.id, true, en49Horas)).toThrow('venció')
})
