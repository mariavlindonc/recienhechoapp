import { useEffect, useState, type FormEvent, type SyntheticEvent } from 'react'
import { useParams } from 'react-router'
import { guardarReceta, obtenerReceta } from '../../api.ts'
import type { Ingrediente } from '../../tipos.ts'
import { mensajeDe, valorDe } from '../../ayudas.ts'

const VACIO = { ingrediente: '', cantidad: '', unidad: 'g' as Ingrediente['unidad'] }

// VE2: ingredientes y cantidades de un producto, por unidad de producto.
export default function Receta() {
  const { id = '' } = useParams()
  const [ingredientes, setIngredientes] = useState<Ingrediente[]>([])
  const [nuevo, setNuevo] = useState(VACIO)
  const [error, setError] = useState('')
  // Actualiza sobre el estado más nuevo: dos cambios seguidos no se pisan.
  const cambiar = (campo: keyof typeof VACIO) => (evento: SyntheticEvent) => {
    const valor = valorDe(evento)
    setNuevo(anteriores => ({ ...anteriores, [campo]: valor }))
  }

  useEffect(() => {
    obtenerReceta(id).then(setIngredientes).catch(errorReceta => setError(mensajeDe(errorReceta)))
  }, [id])

  const agregar = (evento: FormEvent) => {
    evento.preventDefault()
    const actualizada = [...ingredientes, { ...nuevo, cantidad: Number(nuevo.cantidad) }]
    guardarReceta(id, actualizada)
      .then(guardada => {
        setIngredientes(guardada)
        setNuevo(VACIO)
        setError('')
      })
      .catch(errorGuardar => setError(mensajeDe(errorGuardar)))
  }

  return (
    <section>
      <h1>Recetario</h1>
      {error && <p className="aviso-error">{error}</p>}
      <ul>
        {ingredientes.map((linea, indice) => (
          <li key={`${linea.ingrediente}-${indice}`}>{linea.ingrediente}: {linea.cantidad} {linea.unidad}</li>
        ))}
      </ul>
      <form className="formulario" onSubmit={agregar}>
        <md-text-field label="Ingrediente" value={nuevo.ingrediente} onInput={cambiar('ingrediente')} required></md-text-field>
        <md-text-field label="Cantidad" type="number" min="1" value={nuevo.cantidad} onInput={cambiar('cantidad')} required></md-text-field>
        <md-select label="Unidad" value={nuevo.unidad} onChange={cambiar('unidad')}>
          <md-select-option value="g"><div slot="headline">gramos</div></md-select-option>
          <md-select-option value="ml"><div slot="headline">mililitros</div></md-select-option>
          <md-select-option value="unidad"><div slot="headline">unidades</div></md-select-option>
        </md-select>
        <md-button color="filled" type="submit">Agregar ingrediente</md-button>
      </form>
    </section>
  )
}
