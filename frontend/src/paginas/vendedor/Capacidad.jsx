import { useEffect, useState } from 'react'
import { guardarCapacidad, listarCapacidad } from '../../api.js'

// VE3: capacidad de producción por día.
export default function Capacidad() {
  const [dias, setDias] = useState({})
  const [fecha, setFecha] = useState('')
  const [capacidad, setCapacidad] = useState('')
  const [error, setError] = useState('')

  const cargar = () => listarCapacidad().then(setDias)
  useEffect(() => {
    cargar()
  }, [])

  const guardar = evento => {
    evento.preventDefault()
    guardarCapacidad(fecha, Number(capacidad))
      .then(() => {
        setError('')
        cargar()
      })
      .catch(errorGuardar => setError(errorGuardar.message))
  }

  return (
    <section>
      <h1>Capacidad por día</h1>
      {error && <p className="aviso-error">{error}</p>}
      <ul>
        {Object.entries(dias).sort(([primera], [segunda]) => primera.localeCompare(segunda)).map(([dia, valores]) => (
          <li key={dia}>{dia}: {valores.reservados} reservados de {valores.capacidad}</li>
        ))}
      </ul>
      <form className="formulario" onSubmit={guardar}>
        <md-text-field label="Día" type="date" value={fecha} onInput={evento => setFecha(evento.target.value)} required></md-text-field>
        <md-text-field label="Capacidad" type="number" value={capacidad} onInput={evento => setCapacidad(evento.target.value)} required></md-text-field>
        <md-button color="filled" type="submit">Guardar capacidad</md-button>
      </form>
    </section>
  )
}
