import { useState } from 'react'
import { solicitarPedidoEspecial } from '../api.js'

// C6: se pide un pedido a medida; el vendedor lo cotiza después.
export default function PedidoEspecial() {
  const [descripcion, setDescripcion] = useState('')
  const [fecha, setFecha] = useState('')
  const [enviado, setEnviado] = useState(false)

  const enviar = evento => {
    evento.preventDefault()
    solicitarPedidoEspecial({ descripcion, fecha }).then(() => setEnviado(true))
  }

  if (enviado) return <p>Pedimos tu solicitud. Te avisamos cuando la cotizamos.</p>

  return (
    <form className="formulario" onSubmit={enviar}>
      <h1>Pedido especial</h1>
      <md-text-field label="Qué querés" value={descripcion} onInput={evento => setDescripcion(evento.target.value)} required></md-text-field>
      <md-text-field label="Fecha" type="date" value={fecha} onInput={evento => setFecha(evento.target.value)} required></md-text-field>
      <md-button color="filled" type="submit">Solicitar</md-button>
    </form>
  )
}
