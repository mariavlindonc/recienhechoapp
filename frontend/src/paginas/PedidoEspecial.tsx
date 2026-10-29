import { useState, type FormEvent } from 'react'
import { solicitarPedidoEspecial } from '../api.ts'
import { mensajeDe, valorDe } from '../ayudas.ts'

// C6: se pide un pedido a medida; el vendedor lo cotiza después.
export default function PedidoEspecial() {
  const [descripcion, setDescripcion] = useState('')
  const [fecha, setFecha] = useState('')
  const [enviado, setEnviado] = useState(false)
  const [error, setError] = useState('')

  const enviar = (evento: FormEvent) => {
    evento.preventDefault()
    solicitarPedidoEspecial({ descripcion, fecha })
      .then(() => setEnviado(true))
      .catch(errorSolicitud => setError(mensajeDe(errorSolicitud)))
  }

  if (enviado) return <p>Pedimos tu solicitud. Te avisamos cuando la cotizamos.</p>

  return (
    <form className="formulario" onSubmit={enviar}>
      <h1>Pedido especial</h1>
      <md-text-field label="Qué querés" value={descripcion} onInput={evento => setDescripcion(valorDe(evento))} required></md-text-field>
      <md-text-field label="Fecha" type="date" value={fecha} onInput={evento => setFecha(valorDe(evento))} required></md-text-field>
      {error && <p className="aviso-error">{error}</p>}
      <md-button color="filled" type="submit">Solicitar</md-button>
    </form>
  )
}
