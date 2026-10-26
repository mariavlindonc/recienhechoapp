import { useEffect, useState } from 'react'
import { listarPedidosVendedor } from '../../api.ts'
import { formatearFecha } from '../../formato.ts'
import type { Pedido } from '../../tipos.ts'

// VE4: qué hay que producir cada día, agrupado por fecha.
export default function Calendario() {
  const [pedidos, setPedidos] = useState<Pedido[]>([])

  useEffect(() => {
    listarPedidosVendedor().then(setPedidos)
  }, [])

  const activos = pedidos.filter(pedido => !['cancelado', 'entregado'].includes(pedido.estado))
  const porFecha = Object.groupBy(activos, pedido => pedido.fecha)
  const fechas = Object.keys(porFecha).sort()

  return (
    <section>
      <h1>Calendario de producción</h1>
      {fechas.length === 0 && <p>No hay producción pendiente.</p>}
      {fechas.map(fecha => (
        <div key={fecha}>
          <h2>{formatearFecha(fecha)}</h2>
          <ul>
            {porFecha[fecha]?.map(pedido => (
              <li key={pedido.id}>{pedido.cantidad} × {pedido.nombreProducto}</li>
            ))}
          </ul>
        </div>
      ))}
    </section>
  )
}
