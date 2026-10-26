import { createBrowserRouter } from 'react-router'
import Marco from './componentes/Marco.jsx'
import Catalogo from './paginas/Catalogo.jsx'
import DetalleProducto from './paginas/DetalleProducto.jsx'
import Encargar from './paginas/Encargar.jsx'
import PedidoEspecial from './paginas/PedidoEspecial.jsx'
import MisPedidos from './paginas/MisPedidos.jsx'
import Ingresar from './paginas/Ingresar.jsx'
import Registrarse from './paginas/Registrarse.jsx'
import VendedorProductos from './paginas/vendedor/Productos.jsx'
import VendedorReceta from './paginas/vendedor/Receta.jsx'
import VendedorCapacidad from './paginas/vendedor/Capacidad.jsx'
import VendedorCalendario from './paginas/vendedor/Calendario.jsx'
import VendedorEspeciales from './paginas/vendedor/Especiales.jsx'
import VendedorPedidos from './paginas/vendedor/Pedidos.jsx'
import NoEncontrado from './paginas/NoEncontrado.jsx'

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
