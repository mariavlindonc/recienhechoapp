// Escenario de pedidos (ADR-013): 50 usuarios virtuales crean pedidos para el
// mismo día hasta agotar el cupo. Se corre con INVENTARIO_MODO=confirma y lento.
// Los budines de ejemplo piden un día de anticipación, así que por defecto el
// día es pasado mañana, lejos del cambio de fecha en UTC. Cada corrida usa otro
// día (DIAS) para empezar con el cupo libre.
import http from 'k6/http';
import { check } from 'k6';
import { etiquetas, fechaEn, gateway, iniciarSesion, json } from './comun.js';

const usuarios = 50;
const cupo = Number(__ENV.CUPO || 500);
const fecha = fechaEn(Number(__ENV.DIAS || 2));

export const options = {
  scenarios: {
    pedidos: { executor: 'constant-vus', vus: usuarios, duration: __ENV.DURACION || '2m' },
  },
  systemTags: etiquetas,
  thresholds: {
    // RNF3: p95 menor a 2 s, incluida la llamada a inventario.
    'http_req_duration{name:POST /pedidos}': ['p(95)<2000'],
  },
};

export function setup() {
  const vendedor = iniciarSesion(__ENV.VENDEDOR_EMAIL, __ENV.VENDEDOR_CLAVE);
  const ajuste = http.put(`${gateway}/capacidad/${fecha}`, JSON.stringify({ capacidad: cupo }), { headers: vendedor });
  if (ajuste.status !== 200) {
    throw new Error(`no se pudo fijar el cupo de ${fecha}: ${ajuste.status} ${ajuste.body}`);
  }
  const producto = http.get(`${gateway}/productos`).json('productos')[0];
  if (!producto) {
    throw new Error('el catálogo está vacío: correr antes app.sh cargar');
  }

  // Un comprador por usuario virtual; el email cambia en cada corrida.
  const corrida = Date.now();
  const compradores = [];
  for (let numero = 0; numero < usuarios; numero++) {
    const email = `carga-${corrida}-${numero}@recienhecho.test`;
    const clave = 'clave-de-carga-1234';
    const alta = http.post(`${gateway}/usuarios`, JSON.stringify({ nombre: `Carga ${numero}`, email, clave }), { headers: json });
    if (alta.status !== 201) {
      throw new Error(`no se pudo registrar ${email}: ${alta.status} ${alta.body}`);
    }
    compradores.push(iniciarSesion(email, clave));
  }
  return { compradores, productoId: producto.id };
}

export default function (datos) {
  const headers = { ...datos.compradores[(__VU - 1) % usuarios], 'Idempotency-Key': `carga-${__VU}-${__ITER}-${Date.now()}` };
  const cuerpo = JSON.stringify({ tipo: 'programado', items: [{ productoId: datos.productoId, cantidad: 1 }], fecha, franja: 'mañana' });
  const respuesta = http.post(`${gateway}/pedidos`, cuerpo, { headers, tags: { name: 'POST /pedidos' } });
  // 201 mientras hay cupo; después, 409 sin_cupo. Las dos son respuestas correctas.
  check(respuesta, {
    'creado o sin cupo': (resultado) => resultado.status === 201 || (resultado.status === 409 && resultado.json('codigo') === 'sin_cupo'),
  });
}
