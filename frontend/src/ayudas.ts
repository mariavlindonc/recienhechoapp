import type { SyntheticEvent } from 'react'

// Los componentes de material-esm son elementos personalizados: el tipo del evento no trae `value`.
export function valorDe(evento: SyntheticEvent): string {
  return (evento.target as HTMLInputElement).value
}

export function mensajeDe(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

// Intento es un pedido que el comprador está por mandar, con su clave de idempotencia.
export type Intento = { contenido: string; clave: string }

// claveDeIntento mantiene la clave mientras el contenido sea el mismo: si el gateway corta
// por tiempo pero el pedido se guardó, reintentar con la misma clave devuelve ese pedido en
// vez de crear otro (ADR-003). Si el contenido cambia es otro pedido y lleva otra clave.
export function claveDeIntento(anterior: Intento | null, contenido: unknown): Intento {
  const texto = JSON.stringify(contenido)
  if (anterior && anterior.contenido === texto) return anterior
  return { contenido: texto, clave: crypto.randomUUID() }
}
