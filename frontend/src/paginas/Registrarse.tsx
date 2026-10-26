import { Link } from 'react-router'

// C1: registro. Todavía sin autenticación: el formulario no envía nada (ver deuda en ARCHITECTURE.md).
export default function Registrarse() {
  return (
    <form className="formulario" style={{ padding: 16 }} onSubmit={evento => evento.preventDefault()}>
      <h1>Crear cuenta</h1>
      <md-text-field label="Nombre" required></md-text-field>
      <md-text-field label="Email" type="email" required></md-text-field>
      <md-text-field label="Contraseña" type="password" required></md-text-field>
      <md-button color="filled" type="submit">Registrarme</md-button>
      <Link to="/ingresar">Ya tengo cuenta</Link>
    </form>
  )
}
