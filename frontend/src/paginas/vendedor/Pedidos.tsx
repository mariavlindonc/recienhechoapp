import { useEffect, useState } from 'react'
import { avanzarEstado, listarPedidosVendedor, listarPendientes, TRANSICIONES } from '../../api.ts'
import EstadoPedido from '../../componentes/EstadoPedido.tsx'
import type { EstadoPedido as Estado, Pedido, Pendiente } from '../../tipos.ts'
import { mensajeDe } from '../../ayudas.ts'

// Solo los pasos que da el vendedor (VE6). Confirmar y rechazar se hacen arriba, con la nota de lo que falta, y
// cancelar lo hace el comprador (C8).
const ETIQUETAS_SIGUIENTE: Partial<Record<Estado, string>> = {
  en_produccion: 'Pasar a producción',
  listo: 'Marcar listo',
  en_camino: 'Marcar en camino',
  entregado: 'Marcar entregado',
}

// "Faltan 200 g de harina" (ADR-020).
function notaDeFaltantes(pendiente: Pendiente) {
  return `Faltan ${pendiente.faltantes.map(faltante => `${faltante.cantidad} ${faltante.unidad} de ${faltante.ingrediente}`).join(', ')}.`
}

// VE6: confirmar o rechazar los pedidos nuevos y avanzar el estado de los demás por las transiciones válidas (R6).
export default function Pedidos() {
  const [pendientes, setPendientes] = useState<Pendiente[]>([])
  const [pedidos, setPedidos] = useState<Pedido[]>([])
  const [error, setError] = useState('')

  const cargar = () => Promise.all([listarPendientes().then(setPendientes), listarPedidosVendedor().then(setPedidos)])
  useEffect(() => {
    cargar().catch(errorCargar => setError(mensajeDe(errorCargar)))
  }, [])

  const avanzar = (pedido: Pedido, siguiente: Estado) => {
    avanzarEstado(pedido.id, siguiente)
      .then(cargar)
      .catch(errorAvanzar => setError(mensajeDe(errorAvanzar)))
  }

  // Los pendientes ya salen arriba: no se repiten en la lista de abajo.
  const idsPendientes = new Set(pendientes.map(pendiente => pendiente.id))

  return (
    <section>
      <h1>Pedidos</h1>
      {error && <p className="aviso-error">{error}</p>}
      {pendientes.length > 0 && <h2>Esperan tu confirmación</h2>}
      <div className="rejilla">
        {pendientes.map(pendiente => (
          <md-card key={pendiente.id} variant="outlined">
            <div className="formulario" style={{ padding: 16 }}>
              <h3>{pendiente.nombreProducto} × {pendiente.cantidad}</h3>
              <EstadoPedido estado={pendiente.estado} />
              {pendiente.faltantes.length > 0 && <p className="aviso-error">{notaDeFaltantes(pendiente)}</p>}
              <div className="fila">
                <md-button color="filled" onClick={() => avanzar(pendiente, 'confirmado')}>Confirmar</md-button>
                <md-button color="text" onClick={() => avanzar(pendiente, 'cancelado')}>Rechazar</md-button>
              </div>
            </div>
          </md-card>
        ))}
      </div>
      {pedidos.length > 0 && <h2>En curso</h2>}
      <div className="rejilla">
        {pedidos.filter(pedido => !idsPendientes.has(pedido.id)).map(pedido => (
          <md-card key={pedido.id} variant="outlined">
            <div className="formulario" style={{ padding: 16 }}>
              <h3>{pedido.nombreProducto} × {pedido.cantidad}</h3>
              <EstadoPedido estado={pedido.estado} />
              {/* pedidos no pudo descontar los insumos en el proveedor: la producción sigue, pero hay que mirarlo. */}
              {pedido.requiereRevision && <p className="aviso-error">Para revisar: {pedido.motivoRevision}</p>}
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
