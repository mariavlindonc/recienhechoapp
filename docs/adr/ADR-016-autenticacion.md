# ADR-016: Autenticación de usuarios

## Estado

Propuesto el 26/10/2026 con las opciones; la decisión la toma el equipo (regla 7 de AGENTS.md).

## Contexto

El enunciado pide para la presentación un "punto de entrada único con autenticación, direccionamiento y control de tráfico" y un frontend "desde el ingreso del usuario hasta la funcionalidad compartida". El SPEC pide registrarse e iniciar sesión (C1) y guardar las contraseñas hasheadas, sin devolverlas nunca (RNF8). Además, hay dos roles con permisos distintos: comprador y vendedor.

Según [ADR-001](ADR-001-limites-de-los-servicios.md), `usuarios` es dueño de las cuentas y el api-gateway autentica. Hoy no hay nada de eso:

- `usuarios` solo responde `/health`.
- `pedidos` lee la identidad de los headers `X-Id-Usuario`, `X-Email-Usuario` y `X-Rol-Usuario`, y el gateway los pasa tal cual los manda el cliente. Cualquiera puede hacerse pasar por otro usuario ([ARCHITECTURE.md](../ARCHITECTURE.md#limitaciones-conocidas-y-deuda-técnica)).

La autenticación de los grupos que usan `notificaciones` (`X-API-Key`, [ADR-004](ADR-004-contrato-de-notificaciones.md)) no entra en este ADR.

## Decisión

Pendiente. Cualquiera de las opciones de abajo deja igual a `pedidos`: el gateway valida al usuario, borra los headers `X-Id-Usuario`, `X-Email-Usuario` y `X-Rol-Usuario` que mande el cliente y los vuelve a escribir con los datos verificados.

Además de la opción, el equipo tiene que definir:

1. **Hash de contraseñas:** bcrypt o argon2id. Las dos vienen en `golang.org/x/crypto`.
2. **Dónde guarda la sesión el frontend:** en una cookie `HttpOnly` (JavaScript no la puede leer, pero hay que protegerse de CSRF) o en memoria del navegador con un header `Authorization` (más simple, pero un script inyectado en la página la puede leer).
3. **Cuánto dura una sesión** y qué ve el usuario cuando se vence.

## Alternativas consideradas

| Opción | Cómo funciona | A favor | En contra |
|---|---|---|---|
| **A. JWT firmado con un secreto compartido (HS256)** | `usuarios` firma un token con los datos del usuario y el gateway verifica la firma con el mismo secreto. | El gateway no le pregunta nada a `usuarios` en cada solicitud: si `usuarios` se cae, las sesiones abiertas siguen andando. Es lo más simple de implementar. | El secreto está en dos servicios, y con él se pueden fabricar tokens. Un token no se puede revocar antes de que venza, así que la vida tiene que ser corta. Suma una dependencia (por ejemplo, `golang-jwt/jwt/v5`). |
| **B. JWT firmado con un par de claves (RS256 o EdDSA)** | Igual que A, pero `usuarios` firma con la clave privada y el gateway solo tiene la pública. | Si se filtra la configuración del gateway, no se pueden fabricar tokens. | Hay que generar y guardar un par de claves en el `.env`. Mismo problema de revocación y misma dependencia que A. |
| **C. Sesión guardada en `usuarios`** | `usuarios` guarda la sesión y le da al cliente un identificador opaco, que el gateway le consulta a `usuarios` en cada solicitud. | Cerrar sesión o bloquear un usuario surte efecto al instante. No suma dependencias. | Una llamada más por solicitud, y si `usuarios` se cae nadie puede usar el sistema: pasa a ser un punto único de falla para todo, contra RNF4. |

Recomendación: **A**, con una vida del token de 1 hora. Es la más simple y el gateway no depende de que `usuarios` esté arriba; la revocación tardía se acota con la vida corta.

## Consecuencias

Se completan cuando el equipo elija.
