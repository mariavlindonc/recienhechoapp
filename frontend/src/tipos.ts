// Formas de los datos que viajan entre las pantallas y api.ts. Cuando existan los servicios,
// estos tipos son el contrato que hay que respetar (docs/SPEC.md y docs/ARCHITECTURE.md).
// Los montos son centavos enteros (R5).

export type EstadoPedido =
  | 'pendiente_confirmacion'
  | 'confirmado'
  | 'en_produccion'
  | 'listo'
  | 'entregado'
  | 'cancelado'

export type EstadoEspecial = 'solicitado' | 'cotizado' | 'aceptado' | 'rechazado' | 'vencido'

export type Producto = {
  id: string
  nombre: string
  categoria: string
  precioCentavos: number
  anticipacionDias: number
  aptoCeliacos: boolean
  disponible: boolean
  descripcion: string
}

export type DatosProducto = Partial<Producto> &
  Pick<Producto, 'nombre' | 'categoria' | 'precioCentavos' | 'anticipacionDias' | 'aptoCeliacos' | 'descripcion'>

export type Ingrediente = { ingrediente: string; cantidad: string }

export type DiaCapacidad = { capacidad: number; reservados: number }

export type Pedido = {
  id: string
  productoId: string
  nombreProducto: string
  cantidad: number
  fecha: string
  franja?: string
  precioCentavos: number
  tipo: 'programado' | 'inmediato'
  estado: EstadoPedido
}

export type PedidoEspecial = {
  id: string
  descripcion: string
  fecha: string
  estado: EstadoEspecial
  precioCentavos: number | null
  cotizadoEn: string | null
}

export type FiltrosCatalogo = {
  texto?: string
  categoria?: string
  precioMaximo?: number
  aptoCeliacos?: boolean
  orden?: 'nombre' | 'precio-asc' | 'precio-desc'
  pagina?: number
}

export type PaginaDeProductos = { items: Producto[]; total: number; pagina: number }
