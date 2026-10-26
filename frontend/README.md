# Frontend

Interfaz web de Recién hecho: React 19.3, TypeScript, [material-esm](https://github.com/material-esm/material) (Material 3 Expressive) y Bun. Decisiones en [ADR-014](../docs/adr/ADR-014-frontend.md).

## Cómo correrlo

Desde la raíz del repo, con Docker (ver el `README.md` principal):

```bash
docker compose up --build -d
```

Abrir http://localhost:5173.

Para desarrollo local con recarga automática (requiere Bun):

```bash
cd frontend
bun install --frozen-lockfile
bun run dev
```

El `dev` server reenvía `/api` al api-gateway en `API_GATEWAY_URL` (por defecto `http://localhost:8080`).

## Estado

- Las pantallas usan **datos de ejemplo en memoria** (`src/datosDeEjemplo.js`). Todo pasa por `src/api.js`: cuando existan `productos`, `pedidos` y `usuarios`, cada función pasa a `fetch('/api/...')` sin tocar las pantallas.
- La navegación es un `md-nav-rail` en todos los anchos. El plan original preveía `md-nav-bar` debajo de 600 px; se simplificó para no sumar un segundo componente de navegación.
- Sin autenticación: ingreso y registro no envían datos (C1 pendiente). El cambio comprador/vendedor es manual desde la barra superior.
- Los pedidos inmediatos (sin fecha) se rechazan para productos con anticipación mínima mayor a cero, porque la regla R2 exige anticipación.

## Tests y tipos

```bash
bun test
bunx tsc          # chequeo de tipos; `bun run build` también lo corre
```

## Estructura

- `src/rutas.tsx`: tabla de rutas. `src/paginas/`: una página por ruta; `src/paginas/vendedor/`: pantallas del vendedor.
- `src/componentes/`: marco con navegación, tarjeta de producto, estado del pedido.
- `src/tema/`: tokens de color de material-esm (claro y oscuro, copiados del demo del tag `v3.3.11`).
- `servidor.js`: servidor de producción con Bun. Sirve `dist/` y reenvía `/api/*` al api-gateway.
