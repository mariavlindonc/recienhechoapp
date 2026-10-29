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

- Todo pasa por `src/api.ts`, que llama a `usuarios`, `productos` y `pedidos` por `/api` (el api-gateway) y traduce sus respuestas a los tipos de `src/tipos.ts`.
- Sesión: el token de `POST /usuarios/sesion` se guarda en `sessionStorage` y va en `Authorization: Bearer` ([ADR-016](../docs/adr/ADR-016-autenticacion.md)). Un `401` borra la sesión y manda a ingresar. La navegación muestra las pantallas del vendedor solo con una sesión de vendedor (cuenta de desarrollo: `VENDEDOR_EMAIL` y `VENDEDOR_CLAVE` del `.env`).
- Los **pedidos especiales** usan las rutas de `pedidos` de [ADR-017](../docs/adr/ADR-017-pedidos-especiales.md) (rama `feature/pedidos-especiales`). Mientras esperan cotización o respuesta se muestran aparte; aceptados, son un pedido más.
- Las pantallas del vendedor miran desde una semana atrás hasta un mes adelante: `pedidos` lista por día, así que se piden solo los días con algo reservado.
- La navegación es un `md-nav-rail` en todos los anchos. El plan original preveía `md-nav-bar` debajo de 600 px; se simplificó para no sumar un segundo componente de navegación.
- Un pedido sin fecha es inmediato: `pedidos` busca el primer día con cupo que respete la anticipación del producto.

## Tests y tipos

```bash
bun test
bunx tsc          # chequeo de tipos; `bun run build` también lo corre
```

## Estructura

- `src/rutas.tsx`: tabla de rutas. `src/paginas/`: una página por ruta; `src/paginas/vendedor/`: pantallas del vendedor.
- `src/componentes/`: marco con navegación, tarjeta de producto, estado del pedido.
- `src/tema/`: tokens de color de material-esm (claro y oscuro, copiados del demo del tag `v3.3.11`).
- `servidor.ts`: servidor de producción con Bun. Sirve `dist/` y reenvía `/api/*` al api-gateway.
