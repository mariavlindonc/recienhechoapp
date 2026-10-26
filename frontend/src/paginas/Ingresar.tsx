import { Link } from 'react-router'

// C1: ingreso. Todavía sin autenticación: el formulario no envía nada (ver deuda en ARCHITECTURE.md).
export default function Ingresar() {
  return (
    <form className="formulario" style={{ padding: 16 }} onSubmit={evento => evento.preventDefault()}>
      <h1>Ingresar</h1>
      <md-text-field label="Email" type="email" required></md-text-field>
      <md-text-field label="Contraseña" type="password" required></md-text-field>
      <md-button color="filled" type="submit">Ingresar</md-button>
      <Link to="/registrarse">Crear cuenta</Link>
      <Link to="/">Seguir sin cuenta</Link>
    </form>
  )
}
