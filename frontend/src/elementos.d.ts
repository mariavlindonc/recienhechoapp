// Los componentes de material-esm son elementos personalizados: React no los conoce, así que se declaran acá.
import type { DetailedHTMLProps, HTMLAttributes } from 'react'

type ElementoMaterial = DetailedHTMLProps<HTMLAttributes<HTMLElement>, HTMLElement> & Record<string, unknown>

declare module 'react' {
  namespace JSX {
    interface IntrinsicElements {
      'md-app-bar': ElementoMaterial
      'md-button': ElementoMaterial
      'md-card': ElementoMaterial
      'md-chip': ElementoMaterial
      'md-icon': ElementoMaterial
      'md-nav-item': ElementoMaterial
      'md-nav-rail': ElementoMaterial
      'md-select': ElementoMaterial
      'md-select-option': ElementoMaterial
      'md-text-field': ElementoMaterial
    }
  }
}
