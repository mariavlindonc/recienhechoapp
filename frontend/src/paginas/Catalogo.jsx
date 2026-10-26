import { useEffect, useState } from 'react'
import { buscarProductos } from '../api.js'
import TarjetaProducto from '../componentes/TarjetaProducto.jsx'

const CATEGORIAS = ['tortas', 'postres', 'galletitas', 'tartas']

// C2 y C3: catálogo con filtros, orden y paginación de a 20.
export default function Catalogo() {
  const [filtros, setFiltros] = useState({ texto: '', categoria: '', precioMaximo: undefined, aptoCeliacos: false, orden: 'nombre', pagina: 1 })
  const [resultado, setResultado] = useState({ items: [], total: 0 })
  const [error, setError] = useState('')

  useEffect(() => {
    buscarProductos(filtros).then(setResultado).catch(errorBusqueda => setError(errorBusqueda.message))
  }, [filtros])

  const cambiar = cambios => setFiltros(anteriores => ({ ...anteriores, ...cambios, pagina: 1 }))
  const hayMasPaginas = filtros.pagina * 20 < resultado.total

  return (
    <section>
      <h1>Catálogo</h1>
      <div className="fila">
        <md-text-field label="Buscar" value={filtros.texto} onInput={evento => cambiar({ texto: evento.target.value })}></md-text-field>
        <md-text-field label="Precio máximo (pesos)" type="number" onInput={evento => cambiar({ precioMaximo: evento.target.value ? Math.round(Number(evento.target.value) * 100) : undefined })}></md-text-field>
        <md-select label="Categoría" value={filtros.categoria} onChange={evento => cambiar({ categoria: evento.target.value })}>
          <md-select-option value="">
            <div slot="headline">Todas</div>
          </md-select-option>
          {CATEGORIAS.map(categoria => (
            <md-select-option key={categoria} value={categoria}>
              <div slot="headline">{categoria}</div>
            </md-select-option>
          ))}
        </md-select>
        <md-select label="Orden" value={filtros.orden} onChange={evento => cambiar({ orden: evento.target.value })}>
          <md-select-option value="nombre"><div slot="headline">Nombre</div></md-select-option>
          <md-select-option value="precio-asc"><div slot="headline">Precio: menor a mayor</div></md-select-option>
          <md-select-option value="precio-desc"><div slot="headline">Precio: mayor a menor</div></md-select-option>
        </md-select>
        <label>
          <input type="checkbox" checked={filtros.aptoCeliacos} onChange={evento => cambiar({ aptoCeliacos: evento.target.checked })} />
          Apto celíacos
        </label>
      </div>

      {error && <p className="aviso-error">{error}</p>}
      {resultado.items.length === 0 && !error && <p>No hay productos con esos filtros.</p>}

      <div className="rejilla">
        {resultado.items.map(producto => <TarjetaProducto key={producto.id} producto={producto} />)}
      </div>

      <div className="fila">
        <md-button disabled={filtros.pagina === 1} onClick={() => setFiltros(anteriores => ({ ...anteriores, pagina: anteriores.pagina - 1 }))}>Anterior</md-button>
        <span>Página {filtros.pagina}</span>
        <md-button disabled={!hayMasPaginas} onClick={() => setFiltros(anteriores => ({ ...anteriores, pagina: anteriores.pagina + 1 }))}>Siguiente</md-button>
      </div>
    </section>
  )
}
