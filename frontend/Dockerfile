# Versiones exactas (AGENTS.md). Antes de fijar el tag, verificar que tenga al menos dos semanas publicado.
FROM oven/bun:1.4.3-alpine AS compilacion
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY . .
RUN bun run build

FROM oven/bun:1.4.3-alpine
WORKDIR /app
COPY --from=compilacion /app/dist ./dist
COPY servidor.js ./
ENV NODE_ENV=production
CMD ["bun", "servidor.js"]
