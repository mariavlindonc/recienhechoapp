import { useEffect, useState, type FormEvent } from 'react'
import { guardarCapacidad, listarCapacidad } from '../../api.ts'
import type { DiaCapacidad } from '../../tipos.ts'
import { mensajeDe, valorDe } from '../../ayudas.ts'

// VE3: capacidad de producción por día.
export default function Capacidad() {
  const [dias, setDias] = useState<Record<string, DiaCapacidad>>({})
  const [fecha, setFecha] = useState('')
  const [capacidad, setCapacidad] = useState('')
  const [error, setError] = useState('')

  const cargar = () => listarCapacidad().then(setDias)
  useEffect(() => {
    cargar()
  }, [])

  const guardar = (evento: FormEvent) => {
    evento.preventDefault()
    guardarCapacidad(fecha, Number(capacidad))
      .then(() => {
        setError('')
        cargar()
      })
      .catch(errorGuardar => setError(mensajeDe(errorGuardar)))
  }

  return (
    <section>
      <h1>Capacidad por día</h1>
      {error && <p className="aviso-error">{error}</p>}
      <ul>
        {Object.entries(dias).sort(([primera], [segunda]) => primera.localeCompare(segunda)).map(([dia, valores]) => (
          <li key={dia} className="fila">
            <span>{dia}: {valores.reservados} reservados de {valores.capacidad}</span>
            {/* La barra se llena con una transición al guardar una capacidad nueva. */}
            <span className="cupo" aria-hidden="true">
              <span className="cupo-lleno" style={{ display: 'block', transform: `scaleX(${valores.capacidad ? Math.min(valores.reservados / valores.capacidad, 1) : 0})` }} />
            </span>
          </li>
        ))}
      </ul>
      <form className="formulario" onSubmit={guardar}>
        <md-text-field label="Día" type="date" value={fecha} onInput={evento => setFecha(valorDe(evento))} required></md-text-field>
        <md-text-field label="Capacidad" type="number" value={capacidad} onInput={evento => setCapacidad(valorDe(evento))} required></md-text-field>
        <md-button color="filled" type="submit">Guardar capacidad</md-button>
      </form>
    </section>
  )
}
