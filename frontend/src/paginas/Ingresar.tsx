import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router'
import { ingresar } from '../api.ts'
import { mensajeDe, valorDe } from '../ayudas.ts'

// C1: ingreso. El vendedor va a sus pantallas; el comprador, al catálogo.
export default function Ingresar() {
  const navegar = useNavigate()
  const [email, setEmail] = useState('')
  const [clave, setClave] = useState('')
  const [error, setError] = useState('')

  const enviar = (evento: FormEvent) => {
    evento.preventDefault()
    ingresar(email, clave)
      .then(usuario => navegar(usuario.rol === 'vendedor' ? '/vendedor/pedidos' : '/'))
      .catch(errorIngreso => setError(mensajeDe(errorIngreso)))
  }

  return (
    <form className="formulario" style={{ padding: 16 }} onSubmit={enviar}>
      <h1>Ingresar</h1>
      <md-text-field label="Email" type="email" value={email} onInput={evento => setEmail(valorDe(evento))} required></md-text-field>
      <md-text-field label="Contraseña" type="password" value={clave} onInput={evento => setClave(valorDe(evento))} required></md-text-field>
      {error && <p className="aviso-error">{error}</p>}
      <md-button color="filled" type="submit">Ingresar</md-button>
      <Link to="/registrarse">Crear cuenta</Link>
      <Link to="/">Seguir sin cuenta</Link>
    </form>
  )
}
