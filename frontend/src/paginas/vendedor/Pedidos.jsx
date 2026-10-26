import { useEffect, useState } from 'react'
import { avanzarEstado, listarPedidosVendedor, TRANSICIONES } from '../../api.js'
import EstadoPedido from '../../componentes/EstadoPedido.jsx'

// Solo los pasos que da el vendedor (VE6). Confirmar lo hace el inventario (R3) y cancelar, el comprador (C8).
const ETIQUETAS_SIGUIENTE = {
  en_produccion: 'Pasar a producción',
  listo: 'Marcar listo',
  entregado: 'Marcar entregado',
}

// VE6: avanzar el estado de los pedidos por las transiciones válidas (R6).
export default function Pedidos() {
  const [pedidos, setPedidos] = useState([])
  const [error, setError] = useState('')

  const cargar = () => listarPedidosVendedor().then(setPedidos)
  useEffect(() => {
    cargar()
  }, [])

  const avanzar = (pedido, siguiente) => {
    avanzarEstado(pedido.id, siguiente)
      .then(cargar)
      .catch(errorAvanzar => setError(errorAvanzar.message))
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
