import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router'
import { obtenerProducto } from '../api.ts'
import { formatearPrecio } from '../formato.ts'
import type { Producto } from '../tipos.ts'

// C2: detalle del producto con su anticipación mínima antes de pedir.
export default function DetalleProducto() {
  const { id = '' } = useParams()
  const [producto, setProducto] = useState<Producto | null | undefined>(undefined)

  useEffect(() => {
    obtenerProducto(id).then(setProducto)
  }, [id])

  if (producto === undefined) return <p>Cargando…</p>
  if (producto === null) return <p>Ese producto no existe.</p>

  return (
    <section className="formulario" style={{ viewTransitionName: `producto-${producto.id}` }}>
      <span className="etiqueta">{producto.categoria}</span>
      <h1>{producto.nombre}</h1>
      <p className="descripcion">{producto.descripcion}</p>
      <strong>{formatearPrecio(producto.precioCentavos)}</strong>
      <p>Anticipación mínima: {producto.anticipacionDias} días.</p>
      <Link to={`/pedir/${producto.id}`}>
        <md-button color="filled">Encargar</md-button>
      </Link>
    </section>
  )
}
