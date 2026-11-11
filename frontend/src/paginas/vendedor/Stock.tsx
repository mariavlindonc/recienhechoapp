import { useEffect, useState, type FormEvent, type SyntheticEvent } from 'react'
import { cargarStock, listarStock } from '../../api.ts'
import type { Existencia, Ingrediente } from '../../tipos.ts'
import { mensajeDe, valorDe } from '../../ayudas.ts'

const VACIO = { ingrediente: '', cantidad: '', unidad: 'g' as Ingrediente['unidad'] }

// ADR-020: stock propio de cada ingrediente. Cargar suma en el proveedor y guarda lo que este devuelve.
export default function Stock() {
  const [existencias, setExistencias] = useState<Existencia[]>([])
  const [nuevo, setNuevo] = useState(VACIO)
  const [error, setError] = useState('')
  const cambiar = (campo: keyof typeof VACIO) => (evento: SyntheticEvent) => {
    const valor = valorDe(evento)
    setNuevo(anteriores => ({ ...anteriores, [campo]: valor }))
  }

  const cargar = () => listarStock().then(setExistencias)
  useEffect(() => {
    cargar().catch(errorCargar => setError(mensajeDe(errorCargar)))
  }, [])

  const sumar = (evento: FormEvent) => {
    evento.preventDefault()
    cargarStock(nuevo.ingrediente.trim(), nuevo.unidad, Number(nuevo.cantidad))
      .then(() => {
        setNuevo(VACIO)
        setError('')
        return cargar()
      })
      .catch(errorSumar => setError(mensajeDe(errorSumar)))
  }

  return (
    <section>
      <h1>Stock de ingredientes</h1>
      {error && <p className="aviso-error">{error}</p>}
      <ul>
        {existencias.map(existencia => (
          <li key={`${existencia.ingrediente}-${existencia.unidad}`}>
            {existencia.ingrediente}: {existencia.stock} {existencia.unidad}, reservados {existencia.reservado}
            {existencia.disponible < 0 && <strong className="aviso-error"> (faltan {-existencia.disponible} {existencia.unidad})</strong>}
          </li>
        ))}
      </ul>
      <form className="formulario" onSubmit={sumar}>
        <md-text-field label="Ingrediente" value={nuevo.ingrediente} onInput={cambiar('ingrediente')} required></md-text-field>
        <md-text-field label="Cantidad a sumar" type="number" min="1" value={nuevo.cantidad} onInput={cambiar('cantidad')} required></md-text-field>
        <md-select label="Unidad" value={nuevo.unidad} onChange={cambiar('unidad')}>
          <md-select-option value="g"><div slot="headline">gramos</div></md-select-option>
          <md-select-option value="ml"><div slot="headline">mililitros</div></md-select-option>
          <md-select-option value="unidad"><div slot="headline">unidades</div></md-select-option>
        </md-select>
        <md-button color="filled" type="submit">Cargar stock</md-button>
      </form>
    </section>
  )
}
