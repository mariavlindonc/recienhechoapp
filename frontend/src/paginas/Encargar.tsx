import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router'
import { crearPedido, obtenerProducto } from '../api.ts'
import { hoyIso } from '../formato.ts'
import type { Producto } from '../tipos.ts'
import { mensajeDe, valorDe } from '../ayudas.ts'

// C4 y C5: pedido inmediato (sin fecha) o programado (fecha y franja).
export default function Encargar() {
  const { id = '' } = useParams()
  const navegar = useNavigate()
  const [producto, setProducto] = useState<Producto | null | undefined>(undefined)
  const [cantidad, setCantidad] = useState(1)
  const [fecha, setFecha] = useState('')
  const [franja, setFranja] = useState('mañana')
  const [error, setError] = useState('')

  useEffect(() => {
    obtenerProducto(id).then(setProducto)
  }, [id])

  if (producto === undefined) return <p>Cargando…</p>
  if (producto === null) return <p>Ese producto no existe.</p>

  const enviar = (evento: FormEvent) => {
    evento.preventDefault()
    try {
      crearPedido({ productoId: id, cantidad, fecha: fecha || hoyIso(), franja: fecha ? franja : undefined })
        .then(() => navegar('/mis-pedidos'))
        .catch(errorPedido => setError(mensajeDe(errorPedido)))
    } catch (errorPedido) {
      setError(mensajeDe(errorPedido))
    }
  }

  return (
    <form className="formulario" onSubmit={enviar}>
      <h1>Encargar {producto.nombre}</h1>
      <md-text-field label="Cantidad" type="number" min="1" value={cantidad} onInput={evento => setCantidad(Number(valorDe(evento)))}></md-text-field>
      <md-text-field label="Fecha (vacío = para hoy)" type="date" value={fecha} onInput={evento => setFecha(valorDe(evento))}></md-text-field>
      <md-select label="Franja" value={franja} onChange={evento => setFranja(valorDe(evento))} disabled={!fecha}>
        <md-select-option value="mañana"><div slot="headline">Mañana</div></md-select-option>
        <md-select-option value="tarde"><div slot="headline">Tarde</div></md-select-option>
      </md-select>
      {error && <p className="aviso-error">{error}</p>}
      <md-button color="filled" type="submit">Confirmar pedido</md-button>
    </form>
  )
}
