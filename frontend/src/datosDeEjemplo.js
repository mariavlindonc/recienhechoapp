// Datos de ejemplo en memoria. Se reemplazan por los servicios reales cuando existan (ver api.js).
export const productosIniciales = [
  { id: 'p1', nombre: 'Budin 1', categoria: 'budines', precioCentavos: 0, anticipacionDias: 1, aptoCeliacos: false, disponible: true, descripcion: 'Placeholder.' },
  { id: 'p2', nombre: 'Budin 2', categoria: 'budines', precioCentavos: 0, anticipacionDias: 1, aptoCeliacos: false, disponible: true, descripcion: 'Placeholder.' },
  { id: 'p3', nombre: 'Budin 3', categoria: 'budines', precioCentavos: 0, anticipacionDias: 1, aptoCeliacos: false, disponible: true, descripcion: 'Placeholder.' },
]

export const recetasIniciales = {
  p1: [{ ingrediente: 'Harina', cantidad: '300 g' }, { ingrediente: 'Cacao', cantidad: '80 g' }],
  p2: [{ ingrediente: 'Harina', cantidad: '1 kg' }, { ingrediente: 'Manteca', cantidad: '500 g' }],
}

// Capacidad por día: cupos totales y ya reservados.
export const capacidadIniciales = {
  '2026-10-30': { capacidad: 10, reservados: 9 },
  '2026-11-02': { capacidad: 12, reservados: 4 },
}

export const pedidosIniciales = []

export const especialesIniciales = []
