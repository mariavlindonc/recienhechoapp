# ADR-015 (D4): Consistencia y concurrencia

## Estado

- **Aceptado** el uso de transacciones de MongoDB en `pedidos` (26/10/2026).

## Contexto

La operación que no puede duplicar ni perder información es **encargar un pedido** (R1): dos compradores no pueden quedarse con el mismo cupo y un reintento no puede reservar dos veces.

Encargar escribe tres cosas en la base de `pedidos`: el cupo del día, el pedido y el evento para notificaciones. Cancelar escribe las mismas tres al revés. Si el servicio se cae entre una escritura y otra, el sistema queda a medias: cupo descontado sin pedido, o pedido cancelado sin devolver el cupo.

[ADR-002](ADR-002-persistencia.md) resolvió el cupo de un solo documento con un update condicional y dejó para este ADR el caso de varios documentos. [ADR-003](ADR-003-comunicacion-entre-servicios.md) y [ADR-011](ADR-011-resiliencia.md) piden que el evento salga aunque RabbitMQ esté caído.

## Decisión

**Transacciones de MongoDB.** Las escrituras de una misma operación van en una transacción: o se guardan todas o ninguna.

| Operación | Qué guarda la transacción |
|---|---|
| Encargar | Pedido nuevo + reserva condicional del cupo (`reservados + unidades del pedido <= capacidad`) + evento |
| Cambiar de estado | Pedido + evento; al cancelar, también devuelve el cupo |

Para eso, el MongoDB de `pedidos` corre como replica set de un nodo (`--replSet rs0`). El driver reintenta solo la transacción cuando choca con otra que toca el mismo día, y entonces la condición del cupo se vuelve a evaluar con el valor ya actualizado.

**Concurrencia:**

- **Cupo:** la reserva es un update condicional sobre el documento del día. Si no alcanza, no coincide y la transacción entera se deshace. Medido en el test de integración: 20 compradores a la vez contra un cupo de 5 dan exactamente 5 pedidos, 5 eventos y 15 `sin_cupo`.
- **Reintentos del comprador:** índice único en (comprador, `Idempotency-Key`). El segundo intento choca con el índice, no reserva y devuelve el pedido ya creado. La misma clave con otro contenido da `409 conflicto_idempotencia`.
- **Dos cambios a la vez sobre un pedido** (por ejemplo, el comprador cancela mientras el vendedor lo pasa a producción): cada pedido tiene un número de versión que sube en cada cambio. Un cambio sobre una versión vieja no se guarda (`409 modificado_al_mismo_tiempo`), y tampoco su evento ni la devolución del cupo.

**Bandeja de salida (outbox):** el evento se guarda en la colección `eventos_pendientes`, dentro de la misma transacción que el cambio. Un proceso en segundo plano lo publica en RabbitMQ con confirmación del broker y recién ahí lo borra. Con RabbitMQ caído, el pedido se toma igual y el evento espera. Entre que el broker confirma y se borra el evento puede caerse el servicio: en ese caso el evento sale dos veces, y el consumidor lo descarta por `idEvento` (entrega "al menos una vez"). Se publica con `mandatory`: si ninguna cola recibe el evento (por ejemplo, en un entorno nuevo donde `notificaciones` todavía no creó la suya), RabbitMQ lo devuelve en vez de descartarlo y confirmar, y el evento queda en la bandeja hasta que haya una cola (01/11/2026, revisión de [#59](https://github.com/mariavlindonc/recienhechoapp/pull/59)).

Los eventos salen del más viejo al más nuevo. Si publicar uno falla, vuelve a la bandeja como el próximo a tomar, para que al volver el broker no se le adelanten los más nuevos (el comprador no recibe "confirmado" antes que "recibimos tu pedido"). La única excepción es que `pedidos` se caiga mientras tiene un evento apartado: ese evento sale cuando vence el apartado (15 s), y los más nuevos se le pueden adelantar (31/10/2026, revisión de [#59](https://github.com/mariavlindonc/recienhechoapp/pull/59)).

**Insumos:** la reserva de insumos es propia y va dentro de la misma transacción que el pedido, el cupo y el evento: la base `inventario` comparte servidor con la base `pedidos`, y MongoDB permite una transacción entre dos bases del mismo replica set. Cancelar devuelve el cupo, la reserva y guarda el evento juntos, sin llamar al proveedor ([ADR-020](ADR-020-reserva-y-consumo-de-insumos.md)).

**Fallas parciales con el inventario:** solo el consumo (reducir stock al pasar a `en_produccion`) habla con el proveedor, que no participa de la transacción. El pedido guarda su avance por insumo y queda "en duda" antes de cada llamada; si no hubo respuesta, se consulta el stock del proveedor y se compara con el conocido antes de repetir, para no descontar dos veces.

**Valores:** 5 s por transacción, incluidos los reintentos del driver; un pedido o evento tomado por un proceso en segundo plano queda apartado 2 min (pedido) o 15 s (evento) antes de que otro lo pueda volver a tomar.

## Alternativas consideradas

- **Update condicional y compensación, sin transacciones.** Primero se reserva el cupo y después se guarda el pedido con el evento adentro, y un proceso devuelve las reservas huérfanas. No exige replica set, pero suma código de limpieza y deja una ventana con cupo ocupado por un pedido que no existe.
- **Publicar en RabbitMQ directo después de guardar, sin outbox.** Menos código, pero si el servicio se cae entre el guardado y la publicación, el comprador no recibe el aviso.

## Consecuencias

- El MongoDB de `pedidos` necesita inicializar el replica set al arrancar; lo hace el chequeo de salud del compose. Desde afuera del compose hay que conectarse con `?directConnection=true`.
- Los tests de integración necesitan un MongoDB en replica set (`backend/pedidos/internal/adaptadores/mongodb/repositorio_test.go` dice cómo levantarlo).
- Los avisos pueden llegar repetidos si el servicio se cae justo después de publicar; el consumidor los filtra.
- La colección `eventos_pendientes` crece mientras RabbitMQ está caído. Con el volumen esperado no es un problema.
- **Validado bajo carga el 29/10/2026** ([ADR-013](ADR-013-capacidad-y-costos.md#resultados)): 50 usuarios virtuales crearon pedidos para un mismo día con cupo de 500, a unas 860 solicitudes por segundo. En dos corridas se crearon exactamente 500 pedidos, y el día quedó con 500 reservados sobre 500 de capacidad, sin sobreventa. El resto de las solicitudes recibió `409 sin_cupo`.
