// Nombres de docs/ARCHITECTURE.md, "Estados de un pedido", en español para la pantalla.
const ETIQUETAS = {
  pendiente_confirmacion: 'Pendiente de confirmación',
  confirmado: 'Confirmado',
  en_produccion: 'En producción',
  listo: 'Listo',
  entregado: 'Entregado',
  cancelado: 'Cancelado',
}

export default function EstadoPedido({ estado }) {
  return <md-chip type="assist" label={ETIQUETAS[estado] ?? estado}></md-chip>
}
