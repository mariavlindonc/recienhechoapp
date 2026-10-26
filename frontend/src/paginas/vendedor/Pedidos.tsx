import { useEffect, useState } from 'react'
import { avanzarEstado, listarPedidosVendedor, TRANSICIONES } from '../../api.ts'
import EstadoPedido from '../../componentes/EstadoPedido.tsx'
import type { EstadoPedido as Estado, Pedido } from '../../tipos.ts'
import { mensajeDe } from '../../ayudas.ts'

// Solo los pasos que da el vendedor (VE6). Confirmar lo hace el inventario (R3) y cancelar, el comprador (C8).
const ETIQUETAS_SIGUIENTE: Partial<Record<Estado, string>> = {
  en_produccion: 'Pasar a producción',
  listo: 'Marcar listo',
  entregado: 'Marcar entregado',
}

// VE6: avanzar el estado de los pedidos por las transiciones válidas (R6).
export default function Pedidos() {
  const [pedidos, setPedidos] = useState<Pedido[]>([])
  const [error, setError] = useState('')

  const cargar = () => listarPedidosVendedor().then(setPedidos)
  useEffect(() => {
    cargar()
  }, [])

  const avanzar = (pedido: Pedido, siguiente: Estado) => {
    avanzarEstado(pedido.id, siguiente)
      .then(cargar)
      .catch(errorAvanzar => setError(mensajeDe(errorAvanzar)))
  }

  return (
    <section>
      <h1>Pedidos</h1>
      {error && <p className="aviso-error">{error}</p>}
      <div className="rejilla">
        {pedidos.map(pedido => (
          <md-card key={pedido.id} variant="outlined">
            <div className="formulario" style={{ padding: 16 }}>
              <h3>{pedido.nombreProducto} × {pedido.cantidad}</h3>
              <EstadoPedido estado={pedido.estado} />
              {TRANSICIONES[pedido.estado].filter(siguiente => ETIQUETAS_SIGUIENTE[siguiente]).map(siguiente => (
                <md-button key={siguiente} color="tonal" onClick={() => avanzar(pedido, siguiente)}>
                  {ETIQUETAS_SIGUIENTE[siguiente]}
                </md-button>
              ))}
            </div>
          </md-card>
        ))}
      </div>
    </section>
  )
}
