import { createBrowserRouter } from 'react-router'
import Marco from './componentes/Marco.jsx'
import NoEncontrado from './paginas/NoEncontrado.jsx'

export default createBrowserRouter([
  {
    element: <Marco />,
    children: [{ path: '*', element: <NoEncontrado /> }],
  },
])
