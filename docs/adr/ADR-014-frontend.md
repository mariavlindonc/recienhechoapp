# ADR-014: Frontend (React, material-esm y Bun)

## Estado

Propuesto. React 19.3, material-esm y Bun fueron elegidos por Agustín Di Mario el 23/10/2026, y TypeScript el 26/10/2026. Falta que el equipo confirme la decisión y los puntos de abajo marcados como propuestos.

## Contexto

El enunciado pide una interfaz web que centralice toda la interacción del usuario, con el flujo completo desde el ingreso hasta la funcionalidad compartida, para la Presentación Grupal del 11 o 13 de noviembre. El SPEC define el flujo del comprador y del vendedor ([SPEC](../SPEC.md)).

Requisitos que condicionan la elección:
- Material 3 Expressive para el diseño visual (pedido del grupo).
- Bun como gestor de paquetes y runtime, en lugar de npm (regla de la cátedra del grupo; ver `AGENTS.md`).
- El frontend no habla con otros grupos ni con los servicios directamente: todo entra por el api-gateway ([ADR-001](ADR-001-limites-de-los-servicios.md)).
- Hoy `usuarios`, `productos` y `pedidos` son esqueletos, así que el MVP usa datos de ejemplo detrás de un único módulo (`frontend/src/api.js`).

No hay una librería oficial de Google con Material 3 Expressive para web. `material-web` de Google está en pausa y solo cubre Material 3 base. Por eso la elección de librería tiene un costo: depende de un tercero.

## Decisión

- **Framework:** React 19.3.0, con React Router 8.4.0 para las rutas. Construido con Vite 8.3.4.
- **Componentes:** [material-esm](https://github.com/material-esm/material) (fork de `material-web`), fijado al tag `v3.3.11` en `frontend/package.json`. Se importan los componentes que se usan, uno por uno, desde `main.jsx`.
- **Gestor y runtime:** Bun 1.4.3. `bun.lock` queda versionado.
- **Servidor de producción:** un servidor de Bun (`frontend/servidor.ts`) que sirve la app compilada y reenvía `/api/*` al api-gateway. Con esto el navegador habla con un solo origen y no hace falta CORS.
- **Datos:** `src/api.ts` es el único punto de acceso a datos. Llama a los servicios por `/api` y traduce sus respuestas a los tipos de las pantallas.
- **Lenguaje:** TypeScript 7.0.2 en modo `strict`. Los tipos del dominio están en `src/tipos.ts`. `bun run build` corre `tsc` antes de `vite build`. Dependencias de desarrollo nuevas: `typescript`, `@types/react`, `@types/react-dom` y `@types/bun`.
- **Propuesto, sin confirmar por el equipo:** navegación con un `md-nav-rail` en todos los anchos.

## Alternativas consideradas

| Alternativa | A favor | En contra |
|---|---|---|
| **material-esm + React** (elegida) | Componentes web que funcionan con cualquier framework. Expressive casi completo en el fork. Se puede cambiar de framework sin rehacer los componentes | Fork de un tercero, con su README diciendo que Expressive "todavía está en progreso". Sin respaldo de Google |
| Librerías React de Expressive (`m3x`, `md3-expressive-ds`) | React tiene más documentación y ejemplos | Paquetes muy nuevos y sin respaldo de Google. El riesgo de abandono es alto |
| Flutter web con `material_3_expressive` | 45 widgets de Expressive ya hechos | Es Dart y no usa Bun. Expressive lo mantiene la comunidad, no el equipo de Flutter. El bundle es pesado y la accesibilidad web es peor que la de HTML |
| Tokens propios con `material-color-utilities` sobre componentes sin estilo | Sin dependencia de forks. Control total | Mucho trabajo para tres semanas. Expressive se nota sobre todo en formas y animaciones, que son lo más costoso |

## Consecuencias

- Dependemos de un fork de material-esm sin respaldo de Google. Lo fijamos a un tag. Si el fork se abandona, se pasa a la alternativa de tokens propios: se cambian los componentes, no el framework ni las pantallas.
- Algunos componentes de material-esm tienen APIs distintas a las de Material Web original (por ejemplo, `md-button` reemplaza a los botones por variante). Se documentan en el código donde se usan.
- Los datos de ejemplo significan que el MVP no valida la integración real. Eso se demuestra cuando `pedidos` y `productos` tengan implementación.
- Las llamadas a la API pasan por el api-gateway, como pide el enunciado (sección 4.1).
- Las imágenes Docker del frontend (`oven/bun`) y las dependencias del tag se fijan con versión exacta; antes de elegir cada tag se verifica que tenga al menos dos semanas publicado (`AGENTS.md`).

## Limitaciones conocidas

- Sin WebSocket (B2) ni tests e2e (B1).
