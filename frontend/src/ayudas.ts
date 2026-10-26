import type { SyntheticEvent } from 'react'

// Los componentes de material-esm son elementos personalizados: el tipo del evento no trae `value`.
export function valorDe(evento: SyntheticEvent): string {
  return (evento.target as HTMLInputElement).value
}

export function mensajeDe(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
