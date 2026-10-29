// Formas de los datos que viajan entre las pantallas y api.ts. api.ts traduce a estos tipos lo
// que responden los servicios. Los montos son centavos enteros (R5).

export type EstadoPedido =
  | 'pendiente_confirmacion'
  | 'confirmado'
  | 'en_produccion'
  | 'listo'
  | 'entregado'
  | 'cancelado'

export type EstadoEspecial = 'solicitado' | 'cotizado' | 'rechazado' | 'vencido'

export type Producto = {
  id: string
  nombre: string
  categoria: string
  precioCentavos: number
  anticipacionDias: number
  aptoCeliacos: boolean
  disponible: boolean
  descripcion: string
  imagenUrl: string
}

export type DatosProducto = Partial<Producto> &
  Pick<Producto, 'nombre' | 'categoria' | 'precioCentavos' | 'anticipacionDias' | 'aptoCeliacos' | 'descripcion'>

export type Categoria = { id: string; nombre: string }

// Cantidad entera por unidad de producto, en las unidades que acepta productos.
export type Ingrediente = { ingrediente: string; cantidad: number; unidad: 'g' | 'ml' | 'unidad' }

export type Usuario = { id: string; email: string; nombre: string; rol: 'comprador' | 'vendedor' }

export type DiaCapacidad = { capacidad: number; reservados: number }

export type Pedido = {
  id: string
  productoId: string
  nombreProducto: string
  cantidad: number
  fecha: string
  franja?: string
  precioCentavos: number
  tipo: 'programado' | 'inmediato' | 'especial'
  estado: EstadoPedido
}

// Un especial aceptado pasa a ser un Pedido más (pendiente_confirmacion en adelante, ADR-017).
export type PedidoEspecial = {
  id: string
  descripcion: string
  fecha: string
  estado: EstadoEspecial
  precioCentavos: number | null
}

export type FiltrosCatalogo = {
  texto?: string
  categoria?: string
  precioMaximo?: number
  aptoCeliacos?: boolean
  orden?: 'relevancia' | 'precio-asc' | 'precio-desc'
  pagina?: number
}

export type PaginaDeProductos = { items: Producto[]; total: number; pagina: number; porPagina: number }
