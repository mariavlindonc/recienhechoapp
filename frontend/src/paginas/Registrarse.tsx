import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router'
import { registrarse } from '../api.ts'
import { mensajeDe, valorDe } from '../ayudas.ts'

// C1: registro de un comprador; después de crear la cuenta queda con la sesión iniciada.
export default function Registrarse() {
  const navegar = useNavigate()
  const [datos, setDatos] = useState({ nombre: '', email: '', clave: '' })
  const [error, setError] = useState('')

  const enviar = (evento: FormEvent) => {
    evento.preventDefault()
    registrarse(datos)
      .then(() => navegar('/'))
      .catch(errorRegistro => setError(mensajeDe(errorRegistro)))
  }

  return (
    <form className="formulario" style={{ padding: 16 }} onSubmit={enviar}>
      <h1>Crear cuenta</h1>
      <md-text-field label="Nombre" value={datos.nombre} onInput={evento => setDatos({ ...datos, nombre: valorDe(evento) })} required></md-text-field>
      <md-text-field label="Email" type="email" value={datos.email} onInput={evento => setDatos({ ...datos, email: valorDe(evento) })} required></md-text-field>
      <md-text-field label="Contraseña (8 caracteres o más)" type="password" minLength={8} value={datos.clave} onInput={evento => setDatos({ ...datos, clave: valorDe(evento) })} required></md-text-field>
      {error && <p className="aviso-error">{error}</p>}
      <md-button color="filled" type="submit">Registrarme</md-button>
      <Link to="/ingresar">Ya tengo cuenta</Link>
    </form>
  )
}
