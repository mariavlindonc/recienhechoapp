import { Link } from 'react-router'
import { formatearPrecio } from '../formato.ts'
import type { Producto } from '../tipos.ts'

export default function TarjetaProducto({ producto }: { producto: Producto }) {
  return (
    <md-card variant="outlined">
      <div className="formulario" style={{ padding: 16 }}>
        <h3>{producto.nombre}</h3>
        <p>{producto.descripcion}</p>
        <strong>{formatearPrecio(producto.precioCentavos)}</strong>
        <Link to={`/productos/${producto.id}`}>Ver producto</Link>
      </div>
    </md-card>
  )
}
