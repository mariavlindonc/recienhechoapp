import { Link } from 'react-router'

export default function NoEncontrado() {
  return (
    <section>
      <h1>No encontramos esa página</h1>
      <Link to="/">Volver al catálogo</Link>
    </section>
  )
}
