import { expect, test } from 'bun:test'
import { formatearFecha, formatearPrecio, sumarDias } from './formato.js'

test('formatearPrecio muestra pesos a partir de centavos', () => {
  expect(formatearPrecio(185000)).toContain('1.850')
})

test('formatearPrecio rechaza montos con decimales', () => {
  expect(() => formatearPrecio(12.5)).toThrow()
})

test('sumarDias cruza el fin de mes', () => {
  expect(sumarDias('2026-10-30', 3)).toBe('2026-11-02')
})

test('formatearFecha pasa AAAA-MM-DD a DD/MM/AAAA', () => {
  expect(formatearFecha('2026-11-02')).toBe('02/11/2026')
})
