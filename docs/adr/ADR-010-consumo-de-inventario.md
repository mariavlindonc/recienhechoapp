# ADR-010 (D9): Consumo del inventario del proveedor

## Estado

Propuesto (Entrega 2). Todavía no tenemos el contrato del grupo proveedor: la parte que depende de él se completa cuando llegue.

Las tres decisiones sobre los pedidos que el inventario no llega a confirmar las tomó el grupo (María, Athina y Agustín) el 22/10/2026 en [#23](https://github.com/mariavlindonc/recienhechoapp/issues/23).

**Operaciones del proveedor** (decidido por el equipo el 30/10/2026, [#77](https://github.com/mariavlindonc/recienhechoapp/issues/77)): consumimos dos operaciones, **reducir stock** y **aumentar stock**. La versión 2.0.0 de su contrato ([copia](../contracts/inventario/README.md)) es solo de lectura, así que esperamos la versión nueva que las agregue. Lo que depende de esa versión queda en [Pendientes hasta el contrato nuevo](#pendientes-hasta-el-contrato-nuevo).

## Contexto

Consumimos la capacidad de inventario de otro grupo. Un pedido se confirma cuando el proveedor reserva los insumos de la receta (R3), y cancelarlo los libera (R7). El proveedor no ofrece reservas: reservar es reducir el stock de cada insumo, y liberar es volver a aumentarlo. Si el proveedor no responde, el pedido no se rechaza: queda `pendiente_confirmacion` (R3, RNF4).

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
- El adaptador traduce las operaciones del proveedor:

  | Puerto (`pedidos`) | Operación del proveedor |
  |---|---|
  | `Reservar` | Reducir el stock de cada insumo de la receta |
  | `Liberar` | Aumentar el stock en lo mismo que se redujo |

  Los nombres del puerto no cambian: para el dominio sigue siendo reservar y liberar, y la traducción queda en el adaptador, que es para lo que está la capa anticorrupción.
- Reintentar una reserva no puede reservar dos veces. Reducir y aumentar stock no son idempotentes por naturaleza: si el proveedor descontó pero la respuesta no llegó (timeout), reintentar descontaría de nuevo, y consultar el stock antes no alcanza para saberlo porque otros pedidos también lo cambian. Cómo se resuelve depende del contrato nuevo (ver pendientes).
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

**Test de contrato:** la copia versionada del OpenAPI del proveedor está en [`docs/contracts/inventario/`](../contracts/inventario/README.md); cuando publiquen la versión con reducir y aumentar stock, se reemplaza. El test levanta Prism (ya lo usamos como mock propio) con esa especificación y corre el adaptador real contra él. Si el proveedor cambia el contrato de forma incompatible, actualizar la copia hace fallar el test antes de llegar a producción.

**Observabilidad:** métricas de llamadas al proveedor por resultado y latencia, estado del circuit breaker y cantidad de pedidos `pendiente_confirmacion`. Una alerta cuando el proveedor rechaza una reserva por un error de formato: es un error nuestro o un cambio del contrato, y lo tiene que revisar el equipo.

### Pendientes hasta el contrato nuevo

Hay que resolverlos con el grupo proveedor o en el equipo cuando llegue la versión con reducir y aumentar stock:

1. **Idempotencia.** Si reducir y aumentar aceptan una clave (por ejemplo, `idPedido`), reintentar es seguro. Si no, hay que decidir qué hacer ante un timeout, porque reintentar puede descontar dos veces.
2. **Varios insumos.** Si se reducen todos en una sola llamada, o uno por llamada. Con una llamada por insumo, si falla a la mitad hay que aumentar lo que ya se redujo.
3. **Stock insuficiente.** Cómo responde el proveedor, para traducirlo a "sin stock" en la tabla de arriba.
4. **Qué pide aumentar.** Si pide las cantidades, `Liberar` necesita los insumos del pedido además de `idPedido` y cambia la firma del puerto. Si alcanza con una referencia a la reducción, no cambia.
5. **Correspondencia de datos.** Cómo se pasa de un insumo de la receta (ingrediente, cantidad, unidad) al producto del proveedor, y qué sucursal (`branchId`) se usa.
6. **Pedidos sin insumos** (especiales, [ADR-017](ADR-017-pedidos-especiales.md)). Si el proveedor acepta una reducción vacía o si el adaptador no lo llama.
7. **Clave de API.** Va en `.env`, nunca en el repositorio.

## Alternativas consideradas

- **Usar el cliente o los tipos del proveedor directo en el dominio.** Menos código, pero cualquier cambio del proveedor obliga a tocar las reglas de negocio.
- **Cancelar el pedido si el proveedor no responde.** Más simple, pero contradice R3 y RNF4: una caída del proveedor frenaría las ventas.
- **Confirmar sin reservar y reconciliar después.** Vende siempre, pero se pueden prometer pedidos sin insumos.
- **Reintentar cada minuto, siempre igual.** Más simple, pero con el proveedor caído le seguimos pegando todo el tiempo.
- **Cancelar solo a las 24 h y avisarle al comprador.** No necesita intervención, pero se pierden pedidos que el vendedor podría haber salvado.
- **Reintentar también los rechazos por formato.** Uniforme, pero reintentar no arregla un pedido mal armado ni un cambio de contrato, y esconde el problema.
- **Reservar por mensajería.** Desacopla más, pero depende de que el proveedor publique eventos y no sabemos si lo hará.
- **Solo consultar el stock con la versión 2.0.0 (de lectura) y no descontar.** No hay que esperar al proveedor, pero dos pedidos pueden contar con los mismos insumos, porque el stock no baja cuando se confirma uno.

## Consecuencias

- Hay un estado más (`pendiente_confirmacion`) y un proceso de reconfirmación para mantener.
- Mientras el circuito está abierto o el proveedor falla, se toman pedidos con riesgo de no tener insumos; el cupo queda reservado y el vendedor ve esos pedidos marcados.
- Cuando llegue el contrato hay que escribir el adaptador real y el test de contrato; el dominio no cambia (salvo, quizás, la firma de `Liberar`: pendiente 4).
- Reducir no aparta el stock con vencimiento: si un pedido se cancela, los insumos vuelven solo cuando aumentar responde bien. Por eso liberar se reintenta hasta que el proveedor lo confirme ([ADR-015](ADR-015-consistencia-y-concurrencia.md)).
- El vendedor necesita ver en su calendario los pedidos marcados (a las 24 h o por un rechazo de formato) y poder resolverlos a mano.
- Un pedido puede quedar hasta 24 h `pendiente_confirmacion` antes de que el vendedor lo vea marcado; el comprador lo ve en ese estado durante todo ese tiempo.
