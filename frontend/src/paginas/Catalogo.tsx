import { useEffect, useState } from 'react'
import { buscarProductos, listarCategorias } from '../api.ts'
import TarjetaProducto from '../componentes/TarjetaProducto.tsx'
import type { Categoria, FiltrosCatalogo, PaginaDeProductos } from '../tipos.ts'
import { mensajeDe, valorDe } from '../ayudas.ts'

type Filtros = FiltrosCatalogo & { pagina: number }

// C2 y C3: catálogo con filtros, orden y paginación.
export default function Catalogo() {
  const [filtros, setFiltros] = useState<Filtros>({ texto: '', categoria: '', precioMaximo: undefined, aptoCeliacos: false, orden: 'relevancia', pagina: 1 })
  const [resultado, setResultado] = useState<PaginaDeProductos>({ items: [], total: 0, pagina: 1, porPagina: 20 })
  const [categorias, setCategorias] = useState<Categoria[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    listarCategorias().then(setCategorias).catch(errorCategorias => setError(mensajeDe(errorCategorias)))
  }, [])

  useEffect(() => {
    buscarProductos(filtros)
      .then(encontrados => {
        setError('')
        setResultado(encontrados)
      })
      .catch(errorBusqueda => setError(mensajeDe(errorBusqueda)))
  }, [filtros])

  const cambiar = (cambios: Partial<Filtros>) => setFiltros(anteriores => ({ ...anteriores, ...cambios, pagina: 1 }))
  const hayMasPaginas = filtros.pagina * resultado.porPagina < resultado.total

  return (
    <section>
      <h1>Catálogo</h1>
      <div className="fila">
        <md-text-field label="Buscar" value={filtros.texto} onInput={evento => cambiar({ texto: valorDe(evento) })}></md-text-field>
        <md-text-field label="Precio máximo (pesos)" type="number" onInput={evento => cambiar({ precioMaximo: valorDe(evento) ? Math.round(Number(valorDe(evento)) * 100) : undefined })}></md-text-field>
        <md-select label="Categoría" value={filtros.categoria} onChange={evento => cambiar({ categoria: valorDe(evento) })}>
          <md-select-option value="">
            <div slot="headline">Todas</div>
          </md-select-option>
          {categorias.map(categoria => (
            <md-select-option key={categoria.id} value={categoria.nombre}>
              <div slot="headline">{categoria.nombre}</div>
            </md-select-option>
          ))}
        </md-select>
        <md-select label="Orden" value={filtros.orden} onChange={evento => cambiar({ orden: valorDe(evento) as Filtros['orden'] })}>
          <md-select-option value="relevancia"><div slot="headline">Relevancia</div></md-select-option>
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
