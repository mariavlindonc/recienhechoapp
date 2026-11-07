import { useEffect, useState } from 'react'
import { useLocation } from 'react-router'
import { cancelarPedido, listarEspeciales, listarMisPedidos, responderCotizacion, seguirPedidos } from '../api.ts'
import EstadoPedido from '../componentes/EstadoPedido.tsx'
import PrecioQueSube from '../componentes/PrecioQueSube.tsx'
import { formatearFecha, formatearPrecio } from '../formato.ts'
import type { Pedido, PedidoEspecial } from '../tipos.ts'
import { mensajeDe } from '../ayudas.ts'

// C7 y C8: ver pedidos y estado; cancelar solo antes de producción.
export default function MisPedidos() {
  const [pedidos, setPedidos] = useState<Pedido[]>([])
  const [error, setError] = useState('')
  // Encargar manda el id del pedido recién creado para resaltarlo.
  const recienCreado = (useLocation().state as { recienCreado?: string } | null)?.recienCreado

  const [especiales, setEspeciales] = useState<PedidoEspecial[]>([])

  const cargar = () => {
    listarMisPedidos().then(setPedidos)
    listarEspeciales().then(setEspeciales)
  }
  // B2: la lista se actualiza sola cuando cambia un pedido, sin recargar la página.
  useEffect(() => {
    cargar()
    return seguirPedidos(cargar)
  }, [])

  // C6 y R8: el comprador acepta o rechaza la cotización; si pasaron 48 h, se le avisa que venció.
  const responder = (id: string, acepta: boolean) => {
    responderCotizacion(id, acepta)
      .catch(errorResponder => setError(mensajeDe(errorResponder)))
      .finally(cargar)
  }

  const cancelar = (id: string) => {
    cancelarPedido(id)
      .then(cargar)
      .catch(errorCancelar => setError(mensajeDe(errorCancelar)))
  }

  return (
    <section>
      <h1>Mis pedidos</h1>
      {error && <p className="aviso-error">{error}</p>}
      {pedidos.length === 0 && especiales.length === 0 && <p>Todavía no hiciste pedidos.</p>}
      {especiales.length > 0 && <h2>Pedidos especiales</h2>}
      <div className="rejilla">
        {especiales.map(especial => (
          <md-card key={especial.id} variant="outlined">
            <div className="formulario" style={{ padding: 16 }}>
              <h3>{especial.descripcion}</h3>
              <p>Para el {formatearFecha(especial.fecha)} · {especial.estado}</p>
              {especial.precioCentavos !== null &&
                (especial.estado === 'cotizado' ? <PrecioQueSube centavos={especial.precioCentavos} /> : <strong>{formatearPrecio(especial.precioCentavos)}</strong>)}
              {especial.estado === 'cotizado' && (
                <div className="fila">
                  <md-button color="filled" onClick={() => responder(especial.id, true)}>Aceptar</md-button>
                  <md-button color="text" onClick={() => responder(especial.id, false)}>Rechazar</md-button>
                </div>
              )}
              {/* Sin cotizar se retira; ya cotizado se rechaza (ADR-017). */}
              {especial.estado === 'solicitado' && (
                <md-button color="text" onClick={() => cancelar(especial.id)}>Cancelar solicitud</md-button>
              )}
            </div>
          </md-card>
        ))}
      </div>
      {pedidos.length > 0 && <h2>Pedidos</h2>}
      <div className="rejilla">
        {pedidos.map(pedido => (
          <md-card key={pedido.id} variant="outlined" className={pedido.id === recienCreado ? 'recien-creado' : undefined}>
            <div className="formulario" style={{ padding: 16 }}>
              <h3>{pedido.nombreProducto} × {pedido.cantidad}</h3>
              <p>{formatearFecha(pedido.fecha)} {pedido.franja ?? ''}</p>
              {/* Un especial que nunca se cotizó no tiene precio: "$ 0,00" parecería que es gratis. */}
              {(pedido.tipo !== 'especial' || pedido.precioCentavos > 0) && <strong>{formatearPrecio(pedido.precioCentavos)}</strong>}
              <EstadoPedido estado={pedido.estado} />
              {/* Si el vendedor rechazó un especial, su mensaje explica por qué; si no, el motivo de la cancelación. */}
              {pedido.mensajeVendedor ? (
                <p>Mensaje de la pastelería: «{pedido.mensajeVendedor}»</p>
              ) : (
                pedido.estado === 'cancelado' && pedido.motivoCancelacion && <p>{pedido.motivoCancelacion}</p>
              )}
              {['pendiente_confirmacion', 'confirmado'].includes(pedido.estado) && (
                <md-button color="text" onClick={() => cancelar(pedido.id)}>Cancelar</md-button>
              )}
            </div>
          </md-card>
        ))}
      </div>
    </section>
  )
}
