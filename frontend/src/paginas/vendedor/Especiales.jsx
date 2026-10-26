import { useEffect, useState } from 'react'
import { cotizarEspecial, listarEspeciales } from '../../api.js'
import { formatearPrecio } from '../../formato.js'

// VE5: cotizar pedidos especiales. La aceptación la hace el comprador (C6).
export default function Especiales() {
  const [especiales, setEspeciales] = useState([])
  const [precios, setPrecios] = useState({})

  const cargar = () => listarEspeciales().then(setEspeciales)
  useEffect(() => {
    cargar()
  }, [])

  const cotizar = especial => {
    // Pesos a centavos enteros (R5).
    cotizarEspecial(especial.id, Math.round(Number(precios[especial.id]) * 100)).then(cargar)
  }

  return (
    <section>
      <h1>Pedidos especiales</h1>
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
                  <md-text-field label="Precio (pesos)" type="number" value={precios[especial.id] ?? ''} onInput={evento => setPrecios({ ...precios, [especial.id]: evento.target.value })}></md-text-field>
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
