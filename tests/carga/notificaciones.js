// Escenario de notificaciones (ADR-013): POST con claves de idempotencia nuevas
// y GET de la creada. Necesita NOTIFICACIONES_LIMITE_POR_MINUTO alto en el
// gateway: con el límite de 60 por minuto casi todo daría 429.
import http from 'k6/http';
import { check, sleep } from 'k6';
import { etiquetas, gateway, json, rampa } from './comun.js';

const claveAPI = __ENV.CLAVE_API;

export const options = {
  scenarios: { notificaciones: rampa },
  systemTags: etiquetas,
  thresholds: {
    // SLO de ADR-006: el 99 % de los POST en menos de 300 ms.
    'http_req_duration{name:POST /notificaciones}': ['p(99)<300'],
    http_req_failed: ['rate<0.01'],
  },
};

export function setup() {
  if (!claveAPI) {
    throw new Error('falta CLAVE_API (una clave de NOTIFICACIONES_CLAVES_API)');
  }
}

export default function () {
  const headers = { ...json, 'X-API-Key': claveAPI, 'Idempotency-Key': `carga-${__VU}-${__ITER}-${Date.now()}` };
  const cuerpo = JSON.stringify({ canal: 'email', destinatario: 'cliente@ejemplo.com', asunto: 'Prueba de carga', cuerpo: 'Tu pedido está listo.' });
  const creada = http.post(`${gateway}/notificaciones`, cuerpo, { headers, tags: { name: 'POST /notificaciones' } });
  check(creada, { 'POST 202': (respuesta) => respuesta.status === 202 });

  if (creada.status === 202) {
    const consulta = http.get(`${gateway}/notificaciones/${creada.json('id')}`, {
      headers: { 'X-API-Key': claveAPI },
      tags: { name: 'GET /notificaciones/:id' },
    });
    check(consulta, { 'GET 200': (respuesta) => respuesta.status === 200 });
  }
  sleep(1);
}
