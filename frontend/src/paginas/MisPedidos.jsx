import { useEffect, useState } from 'react'
import { cancelarPedido, listarEspeciales, listarMisPedidos, responderCotizacion } from '../api.js'
import EstadoPedido from '../componentes/EstadoPedido.jsx'
import { formatearFecha, formatearPrecio } from '../formato.js'

// C7 y C8: ver pedidos y estado; cancelar solo antes de producción.
export default function MisPedidos() {
  const [pedidos, setPedidos] = useState([])
  const [error, setError] = useState('')

  const [especiales, setEspeciales] = useState([])

  const cargar = () => {
    listarMisPedidos().then(setPedidos)
    listarEspeciales().then(setEspeciales)
  }
  useEffect(() => {
    cargar()
  }, [])

  // C6 y R8: el comprador acepta o rechaza la cotización; si pasaron 48 h, se le avisa que venció.
  const responder = (id, acepta) => {
    try {
      responderCotizacion(id, acepta)
        .then(cargar)
        .catch(errorResponder => setError(errorResponder.message))
    } catch (errorResponder) {
      setError(errorResponder.message)
      cargar()
    }
  }

  const cancelar = id => {
    cancelarPedido(id)
      .then(cargar)
      .catch(errorCancelar => setError(errorCancelar.message))
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
              {especial.precioCentavos !== null && <strong>{formatearPrecio(especial.precioCentavos)}</strong>}
              {especial.estado === 'cotizado' && (
                <div className="fila">
                  <md-button color="filled" onClick={() => responder(especial.id, true)}>Aceptar</md-button>
                  <md-button color="text" onClick={() => responder(especial.id, false)}>Rechazar</md-button>
                </div>
              )}
            </div>
          </md-card>
        ))}
      </div>
      {pedidos.length > 0 && <h2>Pedidos</h2>}
      <div className="rejilla">
        {pedidos.map(pedido => (
          <md-card key={pedido.id} variant="outlined">
            <div className="formulario" style={{ padding: 16 }}>
              <h3>{pedido.nombreProducto} × {pedido.cantidad}</h3>
              <p>{formatearFecha(pedido.fecha)} {pedido.franja ?? ''}</p>
              <strong>{formatearPrecio(pedido.precioCentavos)}</strong>
              <EstadoPedido estado={pedido.estado} />
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
