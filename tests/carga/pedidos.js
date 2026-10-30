// Escenario de pedidos (ADR-013): 50 usuarios virtuales crean pedidos para el
// mismo día hasta agotar el cupo. Se corre con INVENTARIO_MODO=confirma y lento.
// Los budines de ejemplo piden un día de anticipación, así que por defecto el
// día es pasado mañana, lejos del cambio de fecha en UTC. Cada corrida usa otro
// día (DIAS) para empezar con el cupo libre.
import http from 'k6/http';
import { check } from 'k6';
import { Trend } from 'k6/metrics';
import { etiquetas, fechaEn, gateway, iniciarSesion, json } from './comun.js';

const usuarios = 50;
const cupo = Number(__ENV.CUPO || 500);
const dias = Number(__ENV.DIAS || 2);
const fecha = fechaEn(dias);

// Duración solo de los pedidos creados (201): los 409 sin_cupo, que son casi
// todas las respuestas y tardan unos 40 ms, taparían una creación lenta.
const creacion = new Trend('pedido_creado_duracion', true);

export const options = {
  scenarios: {
    pedidos: { executor: 'constant-vus', vus: usuarios, duration: __ENV.DURACION || '2m' },
  },
  systemTags: etiquetas,
  thresholds: {
    // RNF3: crear un pedido, p95 menor a 2 s, incluida la llamada a inventario.
    pedido_creado_duracion: ['p(95)<2000'],
  },
};

export function setup() {
  // Los controles van antes de fijar el cupo, para no tocar los datos si el test no puede correr.
  const vendedor = iniciarSesion(__ENV.VENDEDOR_EMAIL, __ENV.VENDEDOR_CLAVE);
  const dia = http.get(`${gateway}/capacidad?desde=${fecha}&hasta=${fecha}`, { headers: vendedor }).json(fecha);
  // Con reservas de antes, todo daría 409 y el test pasaría sin crear nada.
  if (dia && dia.reservados > 0) {
    throw new Error(`${fecha} ya tiene ${dia.reservados} reservas: usar otro día con -e DIAS=<número>`);
  }
  const producto = http.get(`${gateway}/productos`).json('productos').find((candidato) => candidato.anticipacionMinimaDias <= dias);
  if (!producto) {
    throw new Error(`no hay productos que se puedan encargar con ${dias} días de anticipación: correr antes app.sh cargar o subir DIAS`);
  }
  const ajuste = http.put(`${gateway}/capacidad/${fecha}`, JSON.stringify({ capacidad: cupo }), { headers: vendedor });
  if (ajuste.status !== 200) {
    throw new Error(`no se pudo fijar el cupo de ${fecha}: ${ajuste.status} ${ajuste.body}`);
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
  if (respuesta.status === 201) {
    creacion.add(respuesta.timings.duration);
  }
  // 201 mientras hay cupo; después, 409 sin_cupo. Las dos son respuestas correctas.
  check(respuesta, {
    'creado o sin cupo': (resultado) => resultado.status === 201 || (resultado.status === 409 && resultado.json('codigo') === 'sin_cupo'),
  });
}
