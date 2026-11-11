// Datos del usuario para las pantallas. La sesión de verdad es la cookie HttpOnly, que JavaScript
// no puede leer (ADR-019): acá nunca se guarda el token. Se guarda en sessionStorage: sobrevive a
// recargar la página y se borra al cerrar la pestaña.
import type { Usuario } from './tipos.ts'

const CLAVE = 'sesion'

export type Sesion = { usuario: Usuario }

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
