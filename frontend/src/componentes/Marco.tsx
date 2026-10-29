import { Outlet, useLocation, useNavigate } from 'react-router'
import { cerrarSesion } from '../api.ts'
import { leerSesion } from '../sesion.ts'

const DESTINOS_COMPRADOR = [
  { ruta: '/', etiqueta: 'Catálogo', icono: 'storefront' },
  { ruta: '/mis-pedidos', etiqueta: 'Mis pedidos', icono: 'receipt_long' },
  { ruta: '/pedido-especial', etiqueta: 'Pedido especial', icono: 'cake' },
]

const DESTINOS_VENDEDOR = [
  { ruta: '/vendedor/productos', etiqueta: 'Productos', icono: 'inventory_2' },
  { ruta: '/vendedor/capacidad', etiqueta: 'Capacidad', icono: 'event_available' },
  { ruta: '/vendedor/calendario', etiqueta: 'Calendario', icono: 'calendar_month' },
  { ruta: '/vendedor/especiales', etiqueta: 'Especiales', icono: 'request_quote' },
  { ruta: '/vendedor/pedidos', etiqueta: 'Pedidos', icono: 'bakery_dining' },
]

// La navegación depende del rol de la sesión: el vendedor ve sus pantallas y el resto, las del comprador.
export default function Marco() {
  const { pathname } = useLocation()
  const navegar = useNavigate()
  const sesion = leerSesion()
  const destinos = sesion?.usuario.rol === 'vendedor' ? DESTINOS_VENDEDOR : DESTINOS_COMPRADOR
  const indiceActivo = Math.max(0, destinos.findIndex(destino => pathname === destino.ruta))

  const salir = () => {
    cerrarSesion()
    navegar('/ingresar')
  }

  return (
    <>
      <md-app-bar>
        <div slot="title">Recién hecho</div>
        {sesion ? (
          <md-button slot="end" color="text" onClick={salir}>
            Salir ({sesion.usuario.nombre})
          </md-button>
        ) : (
          <md-button slot="end" color="tonal" onClick={() => navegar('/ingresar')}>
            Ingresar
          </md-button>
        )}
      </md-app-bar>
      <div className="marco">
        <md-nav-rail active-index={indiceActivo}>
          {destinos.map(destino => (
            <md-nav-item key={destino.ruta} label={destino.etiqueta} onClick={() => navegar(destino.ruta)}>
              <md-icon slot="active-icon">{destino.icono}</md-icon>
              <md-icon slot="inactive-icon">{destino.icono}</md-icon>
            </md-nav-item>
          ))}
        </md-nav-rail>
        <main className="contenido">
          <Outlet />
        </main>
      </div>
    </>
  )
}
