# ADR-007 (D2): Arquitectura interna de los servicios

## Estado

- **Aceptado** para `notificaciones` (hexagonal) y `api-gateway`, Entrega 2.
- **Aceptado** para `pedidos` (hexagonal), implementado el 26/10/2026.
- **Propuesto** para `productos` y `usuarios`: se acepta al implementar cada servicio.

Implementación: `notificaciones` y `pedidos` (26/10/2026) ya tienen esta estructura. `usuarios` y `productos` todavía solo exponen `/health`.

## Contexto

El enunciado pide que al menos dos servicios usen patrones de arquitectura interna distintos, vistos en la materia, y que cada elección se justifique según el problema del servicio.

Los servicios no tienen la misma complejidad:

- **notificaciones** y **pedidos** tienen reglas de negocio propias (idempotencia, estados, reintentos, cupo, transiciones válidas) y hablan con varias cosas de afuera: MongoDB, SMTP, RabbitMQ y el inventario de otro grupo. Esas dependencias pueden cambiar (Mailpit o Gmail, mock o inventario real) y hay que poder probar la lógica sin levantarlas.
- **productos** tiene pocas reglas propias, pero depende de tres cosas de afuera: MySQL, el motor de búsqueda y la caché ([ADR-008](ADR-008-busqueda.md), [ADR-009](ADR-009-cache.md)). Cualquiera de las tres se puede cambiar o caer, y hay que poder probar el servicio sin levantarlas.
- **usuarios** es, en su mayoría, altas, bajas, modificaciones y consultas sobre MySQL, con pocas reglas y una sola dependencia.
- **api-gateway** no tiene dominio: autentica, enruta y protege.

## Decisión

**Hexagonal (puertos y adaptadores) en `notificaciones`, `pedidos` y `productos`.** El dominio y los casos de uso no conocen HTTP, las bases, SMTP, RabbitMQ ni el motor de búsqueda: hablan con el exterior por interfaces (puertos) definidas por el propio servicio, y cada tecnología es un adaptador que las implementa. `main.go` arma todo e inyecta los adaptadores.

```
internal/dominio/       entidades, estados, validaciones, errores
internal/aplicacion/    casos de uso
internal/puertos/       interfaces (Repositorio, Remitente, ReservadorDeInsumos, PublicadorDeEventos, Buscador, Cache...)
internal/adaptadores/   api (Gin), mongodb, mysql, smtp, rabbitmq, inventario, meilisearch, redis
```

- En `notificaciones` los puertos son `Repositorio`, `Remitente` y `Eventos`. Cambiar Mailpit por Gmail o agregar el consumidor de RabbitMQ es sumar o cambiar un adaptador, sin tocar el dominio.
- En `pedidos` el puerto `ReservadorDeInsumos` es la capa anticorrupción frente al inventario del otro grupo ([ADR-010](ADR-010-consumo-de-inventario.md)).
- En `productos` los puertos son `Repositorio` (MySQL), `Buscador` (Meilisearch) y `Cache` (Redis). Cambiar de motor de búsqueda es cambiar el adaptador, como prevé [ADR-008](ADR-008-busqueda.md).
- **Logs y métricas también van por un puerto** (revisión del 22/10/2026, decisión de Athina a partir de la revisión del código). La aplicación no importa `slog` ni Prometheus: avisa hechos del negocio por el puerto `Eventos` (por ejemplo, `EnvioIntentado` con el id, el número de intento, el resultado y el error), y el adaptador `eventos` decide qué se cuenta en las métricas y qué se loguea, incluido tapar los emails. Así la regla es una sola: la aplicación habla con el exterior solo por puertos. Las alternativas eran dejar que la aplicación usara `slog` directo (es librería estándar) o pasarle un logger por parámetro; las dos dejaban dos criterios distintos para algo muy parecido.
- Los tests unitarios usan puertos falsos en memoria. Los adaptadores se prueban según lo que cuesta tener la dependencia real: el de MongoDB corre contra un MongoDB de verdad (`MONGO_URI_TEST`), el de SMTP contra un servidor falso en el mismo proceso y el de la API contra casos de uso falsos.

**Capas en `usuarios`:** handler (Gin) → servicio → repositorio (MySQL). Cada capa solo llama a la de abajo. El servicio concentra las validaciones y el repositorio las consultas SQL.

**api-gateway sin capas de dominio:** middlewares de Gin (logs, trazas, autenticación, límite de uso) y un proxy inverso por servicio.

## Alternativas consideradas

- **Hexagonal en todos los servicios.** Uniforme, pero en `usuarios` multiplica interfaces y archivos sin lógica que proteger, y no cumpliría el requisito de dos patrones.
- **Capas en `productos`, llamando directo a Meilisearch y Redis.** Lo más simple, pero para probar el servicio habría que levantar las tres dependencias, y cambiar de motor obligaría a tocar el servicio.
- **Capas en `productos` con interfaces solo para el buscador y la caché.** Menos código que hexagonal, pero mezcla los dos patrones en un mismo servicio y lo vuelve menos reconocible.
- **Capas en todos.** Más simple, pero en `pedidos` y `notificaciones` la lógica terminaría acoplada a MongoDB y a SMTP, y para probar los casos de uso habría que levantar la base o el servidor de correo. Además no cumpliría el requisito de dos patrones.
- **CQRS en `productos`** (escrituras en MySQL, lecturas desde el índice de búsqueda). Encaja con la búsqueda ([ADR-008](ADR-008-busqueda.md)), pero para un catálogo chico la separación formal de modelos agrega más de lo que resuelve. La lectura por el índice se usa igual, pero sin modelos separados de lectura y escritura.

## Consecuencias

- Quedan tres servicios hexagonales y uno en capas. El segundo patrón depende solo de `usuarios`: tiene que estar implementado para la presentación.
- En los servicios hexagonales hay más archivos e interfaces, y hay que cuidar que el dominio no importe paquetes de adaptadores (se revisa en cada PR).
- Si `usuarios` crece en reglas, pasarlo a hexagonal es mover la lógica del servicio a casos de uso y extraer interfaces del repositorio, pero entonces habría que elegir otro servicio para el segundo patrón.
