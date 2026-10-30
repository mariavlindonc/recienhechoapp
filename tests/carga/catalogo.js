// Escenario del catálogo (ADR-013): listado por categoría y detalle. Se corre dos
// veces, con CACHE_HABILITADA=true y false en productos, para comparar (ADR-009).
import http from 'k6/http';
import { check, sleep } from 'k6';
import { etiquetas, gateway, rampa } from './comun.js';

export const options = {
  scenarios: { catalogo: rampa },
  systemTags: etiquetas,
  thresholds: {
    // RNF2: p95 menor a 300 ms con caché.
    'http_req_duration{name:GET /productos?categoria}': ['p(95)<300'],
    'http_req_duration{name:GET /productos/:id}': ['p(95)<300'],
    http_req_failed: ['rate<0.01'],
  },
};

export function setup() {
  const categorias = http.get(`${gateway}/categorias`).json().map((categoria) => categoria.id);
  const productos = http.get(`${gateway}/productos`).json('productos').map((producto) => producto.id);
  if (categorias.length === 0 || productos.length === 0) {
    throw new Error('el catálogo está vacío: correr antes app.sh cargar');
  }
  return { categorias, productos };
}

export default function (datos) {
  const categoria = datos.categorias[__ITER % datos.categorias.length];
  const listado = http.get(`${gateway}/productos?categoria=${categoria}`, { tags: { name: 'GET /productos?categoria' } });
  check(listado, { 'listado 200': (respuesta) => respuesta.status === 200 });

  const producto = datos.productos[__ITER % datos.productos.length];
  const detalle = http.get(`${gateway}/productos/${producto}`, { tags: { name: 'GET /productos/:id' } });
  check(detalle, { 'detalle 200': (respuesta) => respuesta.status === 200 });
  sleep(1);
}
