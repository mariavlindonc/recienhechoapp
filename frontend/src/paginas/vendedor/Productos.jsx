import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { borrarProducto, guardarProducto, listarProductosVendedor } from '../../api.js'
import { formatearPrecio } from '../../formato.js'

const VACIO = { nombre: '', categoria: '', precio: '', anticipacionDias: 1, aptoCeliacos: false, descripcion: '' }

// VE1: alta, baja y modificación de productos.
export default function Productos() {
  const [productos, setProductos] = useState([])
  const [formulario, setFormulario] = useState(VACIO)
  const [error, setError] = useState('')

  const cargar = () => listarProductosVendedor().then(setProductos)
  useEffect(() => {
    cargar()
  }, [])

  const cambiar = cambios => setFormulario(anteriores => ({ ...anteriores, ...cambios }))

  const guardar = evento => {
    evento.preventDefault()
    try {
      guardarProducto({
        id: formulario.id,
        nombre: formulario.nombre,
        categoria: formulario.categoria,
        descripcion: formulario.descripcion,
        anticipacionDias: Number(formulario.anticipacionDias),
        aptoCeliacos: formulario.aptoCeliacos,
        // Pesos a centavos enteros (R5).
        precioCentavos: Math.round(Number(formulario.precio) * 100),
      }).then(() => {
        setFormulario(VACIO)
        cargar()
      })
    } catch (errorGuardar) {
      setError(errorGuardar.message)
    }
  }

  const borrar = id => borrarProducto(id).then(cargar)

  return (
    <section>
      <h1>Productos</h1>
      {error && <p className="aviso-error">{error}</p>}
      <form className="formulario" onSubmit={guardar}>
        <md-text-field label="Nombre" value={formulario.nombre} onInput={evento => cambiar({ nombre: evento.target.value })} required></md-text-field>
        <md-text-field label="Categoría" value={formulario.categoria} onInput={evento => cambiar({ categoria: evento.target.value })} required></md-text-field>
        <md-text-field label="Precio (pesos)" type="number" value={formulario.precio} onInput={evento => cambiar({ precio: evento.target.value })} required></md-text-field>
        <md-text-field label="Anticipación mínima (días)" type="number" value={formulario.anticipacionDias} onInput={evento => cambiar({ anticipacionDias: evento.target.value })} required></md-text-field>
        <label>
          <input type="checkbox" checked={formulario.aptoCeliacos} onChange={evento => cambiar({ aptoCeliacos: evento.target.checked })} />
          Apto celíacos
        </label>
        <md-button color="filled" type="submit">Guardar producto</md-button>
      </form>

      <div className="rejilla">
        {productos.map(producto => (
          <md-card key={producto.id} variant="outlined">
            <div className="formulario" style={{ padding: 16 }}>
              <h3>{producto.nombre}</h3>
              <p>{formatearPrecio(producto.precioCentavos)} · {producto.anticipacionDias} días de anticipación</p>
              <Link to={`/vendedor/productos/${producto.id}/receta`}>Recetario</Link>
              <md-button color="text" onClick={() => borrar(producto.id)}>Dar de baja</md-button>
            </div>
          </md-card>
        ))}
      </div>
    </section>
  )
}
