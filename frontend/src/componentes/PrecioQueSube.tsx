import { useEffect, useRef } from 'react'
import { formatearPrecio } from '../formato.ts'

const DURACION_MS = 600

// El precio de una cotización sube desde 0: es el momento que más espera el comprador.
// Escribe directo en el DOM para no renderizar React en cada cuadro; con "reducir movimiento" no anima.
export default function PrecioQueSube({ centavos }: { centavos: number }) {
  const elemento = useRef<HTMLElement>(null)

  useEffect(() => {
    const destino = elemento.current
    if (!destino || matchMedia('(prefers-reduced-motion: reduce)').matches) return
    let pedidoDeCuadro = 0
    const inicio = performance.now()
    const cuadro = (ahora: number) => {
      const avance = Math.min((ahora - inicio) / DURACION_MS, 1)
      const frenado = 1 - (1 - avance) ** 3
      // Se redondea a pesos enteros mientras sube; el último cuadro muestra el precio exacto.
      destino.textContent = formatearPrecio(avance < 1 ? Math.round((centavos * frenado) / 100) * 100 : centavos)
      if (avance < 1) pedidoDeCuadro = requestAnimationFrame(cuadro)
    }
    pedidoDeCuadro = requestAnimationFrame(cuadro)
    return () => cancelAnimationFrame(pedidoDeCuadro)
  }, [centavos])

  return <strong ref={elemento}>{formatearPrecio(centavos)}</strong>
}
