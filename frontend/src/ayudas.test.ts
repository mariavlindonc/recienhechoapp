import { expect, test } from 'bun:test'
import { claveDeIntento } from './ayudas.ts'

// La clave de idempotencia es la misma mientras el contenido no cambie: reintentar tras un
// error devuelve el pedido ya creado en vez de crear otro (ADR-003).
test('reintentar el mismo pedido usa la misma clave', () => {
  const primero = claveDeIntento(null, { productoId: 'p1', cantidad: 2 })
  const reintento = claveDeIntento(primero, { productoId: 'p1', cantidad: 2 })

  expect(primero.clave).toBeTruthy()
  expect(reintento.clave).toBe(primero.clave)
})

// Si el comprador cambia algo es otro pedido: con la misma clave, pedidos respondería
// 409 conflicto_idempotencia.
test('cambiar el contenido cambia la clave', () => {
  const primero = claveDeIntento(null, { productoId: 'p1', cantidad: 2 })
  const otro = claveDeIntento(primero, { productoId: 'p1', cantidad: 3 })

  expect(otro.clave).not.toBe(primero.clave)
})
