import { createBrowserRouter } from 'react-router'
import Marco from './componentes/Marco.tsx'
import Catalogo from './paginas/Catalogo.tsx'
import DetalleProducto from './paginas/DetalleProducto.tsx'
import Encargar from './paginas/Encargar.tsx'
import PedidoEspecial from './paginas/PedidoEspecial.tsx'
import MisPedidos from './paginas/MisPedidos.tsx'
import Ingresar from './paginas/Ingresar.tsx'
import Registrarse from './paginas/Registrarse.tsx'
import VendedorProductos from './paginas/vendedor/Productos.tsx'
import VendedorReceta from './paginas/vendedor/Receta.tsx'
import VendedorCapacidad from './paginas/vendedor/Capacidad.tsx'
import VendedorCalendario from './paginas/vendedor/Calendario.tsx'
import VendedorEspeciales from './paginas/vendedor/Especiales.tsx'
import VendedorPedidos from './paginas/vendedor/Pedidos.tsx'
import NoEncontrado from './paginas/NoEncontrado.tsx'

export default createBrowserRouter([
  { path: '/ingresar', element: <Ingresar /> },
  { path: '/registrarse', element: <Registrarse /> },
  {
    element: <Marco />,
    children: [
      { path: '/', element: <Catalogo /> },
      { path: '/productos/:id', element: <DetalleProducto /> },
      { path: '/pedir/:id', element: <Encargar /> },
      { path: '/pedido-especial', element: <PedidoEspecial /> },
      { path: '/mis-pedidos', element: <MisPedidos /> },
      { path: '/vendedor/productos', element: <VendedorProductos /> },
      { path: '/vendedor/productos/:id/receta', element: <VendedorReceta /> },
      { path: '/vendedor/capacidad', element: <VendedorCapacidad /> },
      { path: '/vendedor/calendario', element: <VendedorCalendario /> },
      { path: '/vendedor/especiales', element: <VendedorEspeciales /> },
      { path: '/vendedor/pedidos', element: <VendedorPedidos /> },
      { path: '*', element: <NoEncontrado /> },
    ],
  },
])
