import type { CSSProperties } from 'react'
import { Link } from 'react-router'
import { formatearPrecio } from '../formato.ts'
import type { Producto } from '../tipos.ts'

// orden escalona la entrada en la rejilla; el nombre de la transición une la tarjeta con su detalle.
export default function TarjetaProducto({ producto, orden = 0 }: { producto: Producto; orden?: number }) {
  return (
    <md-card variant="outlined" style={{ '--orden': orden, viewTransitionName: `producto-${producto.id}` } as CSSProperties}>
      <div className="formulario" style={{ padding: 16 }}>
        <span className="etiqueta">{producto.categoria}</span>
        <h3>{producto.nombre}</h3>
        <p className="descripcion">{producto.descripcion}</p>
        <strong>{formatearPrecio(producto.precioCentavos)}</strong>
        <Link to={`/productos/${producto.id}`} viewTransition>
          Ver producto
        </Link>
      </div>
    </md-card>
  )
}
