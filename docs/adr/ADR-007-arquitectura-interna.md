# ADR-007 (D2): Arquitectura interna de los servicios

## Estado

- **Aceptado** para `notificaciones` (hexagonal) y `api-gateway`, Entrega 2.
- **Propuesto** para `pedidos`, `productos` y `usuarios`: se acepta al implementar cada servicio.

Implementación: `notificaciones` ya tiene esta estructura. El api-gateway y los demás servicios todavía solo exponen `/health`.

## Contexto

El enunciado pide que al menos dos servicios usen patrones de arquitectura interna distintos, vistos en la materia, y que cada elección se justifique según el problema del servicio.

Los servicios no tienen la misma complejidad:

- **notificaciones** y **pedidos** tienen reglas de negocio propias (idempotencia, estados, reintentos, cupo, transiciones válidas) y hablan con varias cosas de afuera: MongoDB, SMTP, RabbitMQ y el inventario de otro grupo. Esas dependencias pueden cambiar (Mailpit o Gmail, mock o inventario real) y hay que poder probar la lógica sin levantarlas.
- **productos** y **usuarios** son, en su mayoría, altas, bajas, modificaciones y consultas sobre MySQL, con pocas reglas.
- **api-gateway** no tiene dominio: autentica, enruta y protege.

## Decisión

**Hexagonal (puertos y adaptadores) en `notificaciones` y `pedidos`.** El dominio y los casos de uso no conocen HTTP, MongoDB, SMTP ni RabbitMQ: hablan con el exterior por interfaces (puertos) definidas por el propio servicio, y cada tecnología es un adaptador que las implementa. `main.go` arma todo e inyecta los adaptadores.

```
internal/dominio/       entidades, estados, validaciones, errores
internal/aplicacion/    casos de uso
internal/puertos/       interfaces (Repositorio, Remitente, ReservadorDeInsumos, PublicadorDeEventos...)
internal/adaptadores/   api (Gin), mongodb, smtp, rabbitmq, inventario
```

- En `notificaciones` los puertos son `Repositorio` y `Remitente`. Cambiar Mailpit por Gmail o agregar el consumidor de RabbitMQ es sumar o cambiar un adaptador, sin tocar el dominio.
- En `pedidos` el puerto `ReservadorDeInsumos` es la capa anticorrupción frente al inventario del otro grupo ([ADR-010](ADR-010-consumo-de-inventario.md)).
- Los tests unitarios usan puertos falsos en memoria. Los adaptadores se prueban según lo que cuesta tener la dependencia real: el de MongoDB corre contra un MongoDB de verdad (`MONGO_URI_TEST`), el de SMTP contra un servidor falso en el mismo proceso y el de la API contra casos de uso falsos.

**Capas en `productos` y `usuarios`:** handler (Gin) → servicio → repositorio (MySQL). Cada capa solo llama a la de abajo. El servicio concentra las validaciones y el repositorio las consultas SQL.

`productos` suma Meilisearch y Redis ([ADR-008](ADR-008-busqueda.md), [ADR-009](ADR-009-cache.md)), pero como aceleradores de lectura sin reglas de negocio propias. El servicio los usa por dos interfaces chicas que define él mismo, `Buscador` y `Cache`, con un adaptador cada una: así los tests no los necesitan y cambiar de motor es cambiar el adaptador. Es la única concesión; no hay dominio ni casos de uso separados.

**api-gateway sin capas de dominio:** middlewares de Gin (logs, trazas, autenticación, límite de uso) y un proxy inverso por servicio.

## Alternativas consideradas

- **Hexagonal en todos los servicios.** Uniforme, pero en `productos` y `usuarios` multiplica interfaces y archivos sin lógica que proteger.
- **Capas en todos.** Más simple, pero en `pedidos` y `notificaciones` la lógica terminaría acoplada a MongoDB y a SMTP, y para probar los casos de uso habría que levantar la base o el servidor de correo. Además no cumpliría el requisito de dos patrones.
- **CQRS en `productos`** (escrituras en MySQL, lecturas desde el índice de búsqueda). Encaja con la búsqueda ([ADR-008](ADR-008-busqueda.md)), pero para un catálogo chico la separación formal de modelos agrega más de lo que resuelve. La lectura por el índice se usa igual, pero sin modelos separados de lectura y escritura.

## Consecuencias

- Los dos patrones quedan reconocibles: dos servicios hexagonales y dos en capas.
- En los servicios hexagonales hay más archivos e interfaces, y hay que cuidar que el dominio no importe paquetes de adaptadores (se revisa en cada PR).
- Si `productos` o `usuarios` crecen en reglas, pasarlos a hexagonal es mover la lógica del servicio a casos de uso y extraer interfaces del repositorio.
