import { useEffect, useState } from 'react'
import { cotizarEspecial, listarEspeciales } from '../../api.ts'
import { formatearPrecio } from '../../formato.ts'
import type { PedidoEspecial } from '../../tipos.ts'
import { mensajeDe, valorDe } from '../../ayudas.ts'

// VE5: cotizar pedidos especiales. La aceptación la hace el comprador (C6).
export default function Especiales() {
  const [especiales, setEspeciales] = useState<PedidoEspecial[]>([])
  const [precios, setPrecios] = useState<Record<string, string>>({})
  const [error, setError] = useState('')

  const cargar = () => listarEspeciales().then(setEspeciales).catch(errorListar => setError(mensajeDe(errorListar)))
  useEffect(() => {
    cargar()
  }, [])

  const cotizar = (especial: PedidoEspecial) => {
    // Pesos a centavos enteros (R5).
    cotizarEspecial(especial.id, Math.round(Number(precios[especial.id]) * 100))
      .then(cargar)
      .catch(errorCotizar => setError(mensajeDe(errorCotizar)))
  }

  return (
    <section>
      <h1>Pedidos especiales</h1>
      {error && <p className="aviso-error">{error}</p>}
      {especiales.length === 0 && <p>No hay solicitudes.</p>}
      <div className="rejilla">
        {especiales.map(especial => (
          <md-card key={especial.id} variant="outlined">
            <div className="formulario" style={{ padding: 16 }}>
              <h3>{especial.descripcion}</h3>
              <p>Para el {especial.fecha} · {especial.estado}</p>
              {especial.precioCentavos !== null && <strong>{formatearPrecio(especial.precioCentavos)}</strong>}
              {especial.estado === 'solicitado' && (
                <>
                  <md-text-field label="Precio (pesos)" type="number" value={precios[especial.id] ?? ''} onInput={evento => {
                    const valor = valorDe(evento)
                    setPrecios(anteriores => ({ ...anteriores, [especial.id]: valor }))
                  }}></md-text-field>
                  <md-button color="filled" onClick={() => cotizar(especial)}>Cotizar</md-button>
                </>
              )}
            </div>
          </md-card>
        ))}
      </div>
    </section>
  )
}
