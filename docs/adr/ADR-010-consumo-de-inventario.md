# ADR-010 (D9): Consumo del inventario del proveedor

## Estado

Propuesto (Entrega 2). Todavía no tenemos el contrato del grupo proveedor: la parte que depende de él se completa cuando llegue.

Las tres decisiones sobre los pedidos que el inventario no llega a confirmar las tomó el grupo (María, Athina y Agustín) el 22/10/2026 en [#23](https://github.com/mariavlindonc/recienhechoapp/issues/23).

## Contexto

Consumimos la capacidad de inventario de otro grupo. Un pedido se confirma cuando el proveedor reserva los insumos de la receta (R3), y cancelarlo los libera (R7). Si el proveedor no responde, el pedido no se rechaza: queda `pendiente_confirmacion` (R3, RNF4).

El enunciado pide que la integración salga del servicio responsable (no del frontend ni del gateway), que respete el contrato publicado, que tenga un test de contrato, timeouts y tratamiento de errores, y que se defina qué ve el usuario cuando el proveedor falla.

## Decisión

**Puerto propio como capa anticorrupción.** `pedidos` define la interfaz `ReservadorDeInsumos` en sus términos:

```go
type ReservadorDeInsumos interface {
    Reservar(contexto context.Context, idPedido string, insumos []Insumo) (ResultadoReserva, error)
    Liberar(contexto context.Context, idPedido string) error
}
```

- El dominio solo conoce esta interfaz. El formato, los nombres y los errores del proveedor se traducen en el adaptador.
- `idPedido` se manda como clave de idempotencia: reintentar una reserva no reserva dos veces. Si el contrato del proveedor no tiene idempotencia, el adaptador consulta antes de reintentar.
- Mientras no llegue el contrato, se usa un adaptador falso configurable (responde bien, sin stock, lento o con error), que sirve para los tests y para la demo.

**Tiempos y errores:**

| Situación | Qué hace `pedidos` | Qué ve el comprador |
|---|---|---|
| Reserva OK | Pedido `confirmado` | "Pedido confirmado" |
| Sin stock (respuesta de negocio del proveedor) | Libera el cupo del día y cancela el pedido | "No tenemos insumos para esa fecha, probá con otra" |
| Timeout (1,5 s), 5xx o sin conexión | Pedido `pendiente_confirmacion`, cupo reservado | "Recibimos tu pedido, te avisamos por mail cuando se confirme" |
| 4xx inesperado (error nuestro o cambio de contrato) | Pedido `pendiente_confirmacion`, cupo reservado. **Se frena: no se reintenta**, porque reintentar no arregla un error de formato. El equipo recibe una alerta y un log con el detalle del rechazo, y el pedido le aparece marcado al vendedor para que decida, igual que a las 24 h | Igual que timeout |

**Reconfirmación en segundo plano:** un proceso de `pedidos` vuelve a intentar las reservas de los pedidos `pendiente_confirmacion`. Cuando confirma, cambia el estado y publica el evento (el comprador recibe el mail por `notificaciones`). Decidido en [#23](https://github.com/mariavlindonc/recienhechoapp/issues/23):

- **Cada cuánto reintenta:** con esperas que se duplican (1, 2, 4, 8 minutos...) hasta un máximo de 10 minutos entre intentos. Si el proveedor está caído, no se le pega todo el tiempo y tiene aire para recuperarse.
- **Si a las 24 h sigue sin confirmarse:** el pedido le aparece marcado al vendedor y decide él (por ejemplo, conseguir los insumos a mano o cancelarlo). No se cancela solo: en una pastelería chica, el vendedor puede resolverlo mejor que una regla automática.
- **Si el proveedor lo rechaza por un error de formato:** se frena, como dice la tabla de arriba.

**Circuit breaker** sobre el adaptador ([ADR-011](ADR-011-resiliencia.md)): con 5 fallas seguidas se abre por 30 s y los pedidos van directo a `pendiente_confirmacion` sin esperar el timeout.

**Test de contrato:** cuando el proveedor publique su OpenAPI, se guarda una copia versionada en `docs/contracts/proveedor/`. El test levanta Prism (ya lo usamos como mock propio) con esa especificación y corre el adaptador real contra él. Si el proveedor cambia el contrato de forma incompatible, actualizar la copia hace fallar el test antes de llegar a producción.

**Observabilidad:** métricas de llamadas al proveedor por resultado y latencia, estado del circuit breaker y cantidad de pedidos `pendiente_confirmacion`. Una alerta cuando el proveedor rechaza una reserva por un error de formato: es un error nuestro o un cambio del contrato, y lo tiene que revisar el equipo.

## Alternativas consideradas

- **Usar el cliente o los tipos del proveedor directo en el dominio.** Menos código, pero cualquier cambio del proveedor obliga a tocar las reglas de negocio.
- **Cancelar el pedido si el proveedor no responde.** Más simple, pero contradice R3 y RNF4: una caída del proveedor frenaría las ventas.
- **Confirmar sin reservar y reconciliar después.** Vende siempre, pero se pueden prometer pedidos sin insumos.
- **Reintentar cada minuto, siempre igual.** Más simple, pero con el proveedor caído le seguimos pegando todo el tiempo.
- **Cancelar solo a las 24 h y avisarle al comprador.** No necesita intervención, pero se pierden pedidos que el vendedor podría haber salvado.
- **Reintentar también los rechazos por formato.** Uniforme, pero reintentar no arregla un pedido mal armado ni un cambio de contrato, y esconde el problema.
- **Reservar por mensajería.** Desacopla más, pero depende de que el proveedor publique eventos y no sabemos si lo hará.

## Consecuencias

- Hay un estado más (`pendiente_confirmacion`) y un proceso de reconfirmación para mantener.
- Mientras el circuito está abierto o el proveedor falla, se toman pedidos con riesgo de no tener insumos; el cupo queda reservado y el vendedor ve esos pedidos marcados.
- Cuando llegue el contrato hay que escribir el adaptador real y el test de contrato; el dominio no cambia.
- El vendedor necesita ver en su calendario los pedidos marcados (a las 24 h o por un rechazo de formato) y poder resolverlos a mano.
- Un pedido puede quedar hasta 24 h `pendiente_confirmacion` antes de que el vendedor lo vea marcado; el comprador lo ve en ese estado durante todo ese tiempo.
