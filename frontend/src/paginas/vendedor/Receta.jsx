import { useEffect, useState } from 'react'
import { useParams } from 'react-router'
import { guardarReceta, obtenerReceta } from '../../api.js'

// VE2: ingredientes y cantidades de un producto.
export default function Receta() {
  const { id } = useParams()
  const [ingredientes, setIngredientes] = useState([])
  const [nuevo, setNuevo] = useState({ ingrediente: '', cantidad: '' })

  useEffect(() => {
    obtenerReceta(id).then(setIngredientes)
  }, [id])

  const agregar = evento => {
    evento.preventDefault()
    const actualizada = [...ingredientes, nuevo]
    setIngredientes(actualizada)
    setNuevo({ ingrediente: '', cantidad: '' })
    guardarReceta(id, actualizada)
  }

  return (
    <section>
      <h1>Recetario</h1>
      <ul>
        {ingredientes.map((linea, indice) => (
          <li key={`${linea.ingrediente}-${indice}`}>{linea.ingrediente}: {linea.cantidad}</li>
        ))}
      </ul>
      <form className="formulario" onSubmit={agregar}>
        <md-text-field label="Ingrediente" value={nuevo.ingrediente} onInput={evento => setNuevo({ ...nuevo, ingrediente: evento.target.value })} required></md-text-field>
        <md-text-field label="Cantidad" value={nuevo.cantidad} onInput={evento => setNuevo({ ...nuevo, cantidad: evento.target.value })} required></md-text-field>
        <md-button color="filled" type="submit">Agregar ingrediente</md-button>
      </form>
    </section>
  )
}
