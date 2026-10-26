# ADR-015 (D4): Consistencia y concurrencia

## Estado

- **Aceptado** el uso de transacciones de MongoDB en `pedidos` (Agustín, 26/10/2026).
- **Propuesto** por un agente de código, para que el equipo lo confirme en el PR: el control de versión de cada pedido, la bandeja de salida (outbox) y los valores de tiempo. Están marcados con *(propuesto)*.

## Contexto

La operación que no puede duplicar ni perder información es **encargar un pedido** (R1): dos compradores no pueden quedarse con el mismo cupo y un reintento no puede reservar dos veces.

Encargar escribe tres cosas en la base de `pedidos`: el cupo del día, el pedido y el evento para notificaciones. Cancelar escribe las mismas tres al revés. Si el servicio se cae entre una escritura y otra, el sistema queda a medias: cupo descontado sin pedido, o pedido cancelado sin devolver el cupo.

[ADR-002](ADR-002-persistencia.md) resolvió el cupo de un solo documento con un update condicional y dejó para este ADR el caso de varios documentos. [ADR-003](ADR-003-comunicacion-entre-servicios.md) y [ADR-011](ADR-011-resiliencia.md) piden que el evento salga aunque RabbitMQ esté caído.

## Decisión

**Transacciones de MongoDB.** Las escrituras de una misma operación van en una transacción: o se guardan todas o ninguna.

| Operación | Qué guarda la transacción |
|---|---|
| Encargar | Pedido nuevo + reserva condicional del cupo (`reservados + cantidad <= capacidad`) + evento |
| Cambiar de estado | Pedido + evento; al cancelar, también devuelve el cupo |

Para eso, el MongoDB de `pedidos` corre como replica set de un nodo (`--replSet rs0`). El driver reintenta solo la transacción cuando choca con otra que toca el mismo día, y entonces la condición del cupo se vuelve a evaluar con el valor ya actualizado.

**Concurrencia:**

- **Cupo:** la reserva es un update condicional sobre el documento del día. Si no alcanza, no coincide y la transacción entera se deshace. Medido en el test de integración: 20 compradores a la vez contra un cupo de 5 dan exactamente 5 pedidos, 5 eventos y 15 `sin_cupo`.
- **Reintentos del comprador:** índice único en (comprador, `Idempotency-Key`). El segundo intento choca con el índice, no reserva y devuelve el pedido ya creado. La misma clave con otro contenido da `409 conflicto_idempotencia`.
- **Dos cambios a la vez sobre un pedido** (por ejemplo, el comprador cancela mientras el vendedor lo pasa a producción) *(propuesto)*: cada pedido tiene un número de versión que sube en cada cambio. Un cambio sobre una versión vieja no se guarda (`409 modificado_al_mismo_tiempo`), y tampoco su evento ni la devolución del cupo.

**Bandeja de salida (outbox)** *(propuesto)*: el evento se guarda en la colección `eventos_pendientes`, dentro de la misma transacción que el cambio. Un proceso en segundo plano lo publica en RabbitMQ con confirmación del broker y recién ahí lo borra. Con RabbitMQ caído, el pedido se toma igual y el evento espera. Entre que el broker confirma y se borra el evento puede caerse el servicio: en ese caso el evento sale dos veces, y el consumidor lo descarta por `idEvento` (entrega "al menos una vez").

**Fallas parciales con el inventario:** la reserva de insumos va después de la transacción, porque es una llamada a otro sistema que no participa de ella. Si no responde, el pedido queda `pendiente_confirmacion` con su cupo y se reconfirma en segundo plano ([ADR-010](ADR-010-consumo-de-inventario.md)). Liberar insumos al cancelar (R7) se marca en el pedido y se reintenta hasta que el proveedor lo confirme.

**Valores** *(propuesto)*: 5 s por transacción, incluidos los reintentos del driver; un pedido o evento tomado por un proceso en segundo plano queda apartado 2 min (pedido) o 15 s (evento) antes de que otro lo pueda volver a tomar.

## Alternativas consideradas

- **Update condicional y compensación, sin transacciones.** Primero se reserva el cupo y después se guarda el pedido con el evento adentro, y un proceso devuelve las reservas huérfanas. No exige replica set, pero suma código de limpieza y deja una ventana con cupo ocupado por un pedido que no existe.
- **Publicar en RabbitMQ directo después de guardar, sin outbox.** Menos código, pero si el servicio se cae entre el guardado y la publicación, el comprador no recibe el aviso.

## Consecuencias

- El MongoDB de `pedidos` necesita inicializar el replica set al arrancar; lo hace el chequeo de salud del compose. Desde afuera del compose hay que conectarse con `?directConnection=true`.
- Los tests de integración necesitan un MongoDB en replica set (`backend/pedidos/internal/adaptadores/mongodb/repositorio_test.go` dice cómo levantarlo).
- Los avisos pueden llegar repetidos si el servicio se cae justo después de publicar; el consumidor los filtra.
- La colección `eventos_pendientes` crece mientras RabbitMQ está caído. Con el volumen esperado no es un problema.
