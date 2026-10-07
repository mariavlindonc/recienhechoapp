# Recién hecho

E-commerce de pastelería a pedido: los compradores encargan productos y los vendedores organizan su producción.

## Cómo levantarlo

Requisitos: Docker con Compose.

```bash
cp .env.example .env && docker compose up --build -d
```

| Servicio | URL |
|---|---|
| API Gateway | http://localhost:8080/health |
| Usuarios | http://localhost:8081/health |
| Productos | http://localhost:8082/health |
| Pedidos | http://localhost:8083/health |

## Documentación

- [SPEC.md](SPEC.md): requisitos
- [AGENTS.md](AGENTS.md): comandos, convenciones y reglas (también para agentes de código)
- [CONTRIBUTING.md](CONTRIBUTING.md): ramas, commits y pull requests
- [docs/](docs/): arquitectura, ADRs, contratos y postmortems
