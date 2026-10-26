// Los montos son centavos enteros (R5). Solo se convierten a texto en la pantalla.
export function formatearPrecio(centavos) {
  if (!Number.isInteger(centavos)) {
    throw new Error(`el precio debe ser un entero en centavos: ${centavos}`)
  }
  return new Intl.NumberFormat('es-AR', { style: 'currency', currency: 'ARS' }).format(centavos / 100)
}

// Fechas en formato AAAA-MM-DD, en hora local.
export function hoyIso() {
  return fechaIso(new Date())
}

export function sumarDias(fechaIsoTexto, dias) {
  const fecha = new Date(`${fechaIsoTexto}T00:00:00`)
  fecha.setDate(fecha.getDate() + dias)
  return fechaIso(fecha)
}

export function formatearFecha(fechaIsoTexto) {
  const [anio, mes, dia] = fechaIsoTexto.split('-')
  return `${dia}/${mes}/${anio}`
}

function fechaIso(fecha) {
  const anio = fecha.getFullYear()
  const mes = String(fecha.getMonth() + 1).padStart(2, '0')
  const dia = String(fecha.getDate()).padStart(2, '0')
  return `${anio}-${mes}-${dia}`
}
