import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router'
import { crearPedido, obtenerProducto } from '../api.ts'
import { hoyIso } from '../formato.ts'
import type { Producto } from '../tipos.ts'
import { claveDeIntento, mensajeDe, valorDe, type Intento } from '../ayudas.ts'

// C4 y C5: pedido inmediato (sin fecha) o programado (fecha y franja).
export default function Encargar() {
  const { id = '' } = useParams()
  const navegar = useNavigate()
  const [producto, setProducto] = useState<Producto | null | undefined>(undefined)
  const [cantidad, setCantidad] = useState(1)
  const [fecha, setFecha] = useState('')
  const [franja, setFranja] = useState('mañana')
  const [error, setError] = useState('')
  const [enviando, setEnviando] = useState(false)
  // Si "Confirmar" falla y se vuelve a tocar con los mismos datos, va la misma clave: si el
  // primer intento llegó a guardarse, pedidos devuelve ese pedido en vez de crear otro.
  const intento = useRef<Intento | null>(null)

  useEffect(() => {
    obtenerProducto(id).then(setProducto)
  }, [id])

  if (producto === undefined) return <p>Cargando…</p>
  if (producto === null) return <p>Ese producto no existe.</p>

  const enviar = (evento: FormEvent) => {
    evento.preventDefault()
    if (enviando) return
    setEnviando(true)
    const datos = { productoId: id, cantidad, fecha: fecha || undefined, franja: fecha ? franja : undefined }
    intento.current = claveDeIntento(intento.current, datos)
    crearPedido({ ...datos, clave: intento.current.clave })
      // Mis pedidos resalta el que se acaba de crear.
      .then(pedido => navegar('/mis-pedidos', { state: { recienCreado: pedido.id } }))
      .catch(errorPedido => {
        // Sin respuesta (se cortó la red) no se sabe si el pedido llegó: reintentar es seguro
        // porque va la misma clave, y se lo decimos al comprador.
        setError(errorPedido instanceof TypeError ? 'Se cortó la conexión. Volvé a tocar «Confirmar pedido»: si el pedido ya había llegado, no se va a duplicar.' : mensajeDe(errorPedido))
        setEnviando(false)
      })
  }

  return (
    <form className="formulario" onSubmit={enviar}>
      <h1>Encargar {producto.nombre}</h1>
      <md-text-field label="Cantidad" type="number" min="1" value={cantidad} onInput={evento => setCantidad(Number(valorDe(evento)))}></md-text-field>
      <md-text-field label="Fecha (vacío = lo antes posible)" type="date" min={hoyIso()} value={fecha} onInput={evento => setFecha(valorDe(evento))}></md-text-field>
      <md-select label="Franja" value={franja} onChange={evento => setFranja(valorDe(evento))} disabled={!fecha}>
        <md-select-option value="mañana"><div slot="headline">Mañana</div></md-select-option>
        <md-select-option value="tarde"><div slot="headline">Tarde</div></md-select-option>
      </md-select>
      {error && <p className="aviso-error">{error}</p>}
      <md-button color="filled" type="submit" aria-busy={enviando}>{enviando ? 'Encargando…' : 'Confirmar pedido'}</md-button>
    </form>
  )
}
