# Cómo contribuir

## Flujo de ramas (git flow)

| Rama | Sale de | Se mergea en | Para qué |
|---|---|---|---|
| `main` | — | — | Versiones publicadas. Solo recibe `release/*` y `hotfix/*` |
| `develop` | `main` | — | Integración del trabajo en curso |
| `feature/<nombre>` | `develop` | `develop` | Funcionalidades nuevas |
| `bugfix/<nombre>` | `develop` | `develop` | Correcciones sobre lo que todavía no se publicó |
| `release/<versión>` | `develop` | `main` y `develop` | Preparar una versión; al cerrarla se etiqueta `vX.Y.Z` en `main` |
| `hotfix/<versión>` | `main` | `main` y `develop` | Corrección urgente sobre producción; se etiqueta `vX.Y.Z` |

Reglas:

- `main` solo recibe merges de `release/*` y `hotfix/*`.
- Nadie commitea directo a `main` ni a `develop`.
- Todo merge a `develop` pasa por pull request con al menos una revisión humana, incluidos los cambios hechos por agentes de código.
- Merges con `--no-ff` para que quede registrada cada rama.
- Antes de abrir el PR: los tests y el lint de [AGENTS.md](AGENTS.md#comandos) tienen que pasar.

Con [git-flow](https://github.com/petervanderdoes/gitflow-avh) instalado, `git flow init -d` toma esta misma configuración (prefijos `feature/`, `bugfix/`, `release/`, `hotfix/` y tags con prefijo `v`).

## Mensajes de commit

Se usa [Conventional Commits](https://www.conventionalcommits.org/es/):

```
tipo(alcance): descripción breve en imperativo

Cuerpo opcional explicando el porqué.
```

- Tipos: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `build`, `ci`.
- Alcance: el servicio (`usuarios`, `productos`, `pedidos`, `notificaciones`, `api-gateway`) o `infra`, `docs`.
- Cambios incompatibles: `!` después del tipo (`feat(pedidos)!: ...`) y un pie `BREAKING CHANGE:`.

## Varios agentes en paralelo: git worktrees

Cuando varios agentes (o personas) trabajan a la vez, cada uno usa su propio worktree con su rama `feature/`, así no se pisan archivos ni el estado de git:

```bash
git worktree add ../recienhecho-catalogo -b feature/catalogo develop
```

Un worktree por rama `feature/`. Al terminar y mergear el PR:

```bash
git worktree remove ../recienhecho-catalogo
```

Ojo: todos los worktrees comparten los puertos de `.env`; levantar el stack con `docker compose up` desde un solo worktree a la vez.
