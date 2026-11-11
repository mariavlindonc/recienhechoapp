// Lo que comparten los escenarios de carga (ADR-013). Todo pasa por el
// api-gateway, como cualquier cliente.
import http from 'k6/http';

export const gateway = __ENV.GATEWAY_URL || 'http://api-gateway:8080';

// Rampa de 10 a 200 usuarios virtuales en 5 minutos y 5 minutos sostenido (ADR-013).
export const rampa = {
  executor: 'ramping-vus',
  startVUs: 10,
  stages: [
    { duration: '5m', target: 200 },
    { duration: '5m', target: 200 },
  ],
};

export const json = { 'Content-Type': 'application/json' };

// Etiquetas que k6 agrega a cada resultado, sin url: con el id en la URL, cada
// solicitud sería una serie nueva en Prometheus. Las rutas se agrupan con name.
export const etiquetas = ['status', 'method', 'name', 'scenario', 'expected_response', 'check', 'error_code'];

// iniciarSesion devuelve los headers con el token de la cuenta.
export function iniciarSesion(email, clave) {
  const respuesta = http.post(`${gateway}/usuarios/sesion`, JSON.stringify({ email, clave }), { headers: json });
  if (respuesta.status !== 200) {
    throw new Error(`no se pudo iniciar sesión como ${email}: ${respuesta.status} ${respuesta.body}`);
  }
  return { ...json, Authorization: `Bearer ${respuesta.json('token')}` };
}

// fechaEn devuelve la fecha (UTC, como pedidos) dentro de tantos días.
export function fechaEn(dias) {
  return new Date(Date.now() + dias * 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
}
