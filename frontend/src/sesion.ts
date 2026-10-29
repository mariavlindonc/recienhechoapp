// Sesión del usuario: el token va en el header Authorization de cada solicitud (ADR-016).
// Se guarda en sessionStorage: sobrevive a recargar la página y se borra al cerrar la pestaña.
import type { Usuario } from './tipos.ts'

const CLAVE = 'sesion'

export type Sesion = { token: string; usuario: Usuario }

export function leerSesion(): Sesion | null {
  const guardada = sessionStorage.getItem(CLAVE)
  return guardada ? (JSON.parse(guardada) as Sesion) : null
}

export function guardarSesion(sesion: Sesion) {
  sessionStorage.setItem(CLAVE, JSON.stringify(sesion))
}

export function borrarSesion() {
  sessionStorage.removeItem(CLAVE)
}
