import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router'
import rutas from './rutas.tsx'

// Componentes de material-esm que usa la app. Cada uno se registra al importarlo.
import 'material/app/bar.js'
import 'material/buttons/button.js'
import 'material/buttons/icon-button.js'
import 'material/card/card.js'
import 'material/chips/chip.js'
import 'material/chips/chip-set.js'
import 'material/icon/icon.js'
import 'material/list/list.js'
import 'material/list/list-item.js'
import 'material/nav/rail.js'
import 'material/nav/item.js'
import 'material/select/select.js'
import 'material/select/select-option.js'
import 'material/text/text-field.js'

import './tema/tema.css'

createRoot(document.getElementById('raiz')!).render(
  <StrictMode>
    <RouterProvider router={rutas} />
  </StrictMode>,
)
