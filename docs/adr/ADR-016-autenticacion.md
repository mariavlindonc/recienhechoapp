# ADR-016: Autenticación de usuarios

## Estado

Propuesto el 26/10/2026 con las opciones. Aceptado por Agustín el 28/10/2026: opción A, bcrypt y vendedor creado desde el `.env`. El mismo día eligió el header `Authorization` para la sesión del frontend.

## Contexto

El enunciado pide para la presentación un "punto de entrada único con autenticación, direccionamiento y control de tráfico" y un frontend "desde el ingreso del usuario hasta la funcionalidad compartida". El SPEC pide registrarse e iniciar sesión (C1) y guardar las contraseñas hasheadas, sin devolverlas nunca (RNF8). Además, hay dos roles con permisos distintos: comprador y vendedor.

Según [ADR-001](ADR-001-limites-de-los-servicios.md), `usuarios` es dueño de las cuentas y el api-gateway autentica. Hasta este ADR, `pedidos` lee la identidad de los headers `X-Id-Usuario`, `X-Email-Usuario` y `X-Rol-Usuario`, y el gateway los pasa tal cual los manda el cliente: cualquiera puede hacerse pasar por otro usuario ([ARCHITECTURE.md](../ARCHITECTURE.md#limitaciones-conocidas-y-deuda-técnica)).

La autenticación de los grupos que usan `notificaciones` (`X-API-Key`, [ADR-004](ADR-004-contrato-de-notificaciones.md)) no entra en este ADR.

## Decisión

**A. JWT firmado con un secreto compartido (HS256), con una vida de 1 hora.** `usuarios` lo firma al iniciar sesión y el api-gateway verifica la firma con el mismo secreto (`JWT_SECRETO`, de al menos 32 bytes). El gateway borra los headers `X-Id-Usuario`, `X-Email-Usuario` y `X-Rol-Usuario` que mande el cliente y los vuelve a escribir con los datos del token, así `pedidos` y `productos` no cambian.

El token lleva `sub` (id del usuario), `email`, `rol`, `iat` y `exp`. Dependencia: `github.com/golang-jwt/jwt/v5` v5.3.1.

**Contraseñas con bcrypt** (`golang.org/x/crypto/bcrypt`, costo por defecto, 10). bcrypt ignora lo que pasa de 72 bytes, así que la contraseña se valida entre 8 y 72 bytes.

**Vendedor desde el `.env`.** El registro público crea solo compradores. Al arrancar, `usuarios` crea la cuenta del vendedor con `VENDEDOR_EMAIL` y `VENDEDOR_CLAVE` si no existe; si existe, no la toca. Así el sistema se levanta sin pasos manuales (RNF7).

**Rutas de `usuarios`:**

| Ruta | Qué hace | Respuestas |
|---|---|---|
| `POST /usuarios` | Registro de un comprador (`email`, `nombre`, `clave`) | `201`, `400 solicitud_invalida`, `409 email_repetido` |
| `POST /usuarios/sesion` | Inicio de sesión (`email`, `clave`); devuelve `token`, `venceEn` y el usuario | `200`, `401 credenciales_invalidas` |
| `GET /usuarios/yo` | Perfil del usuario de `X-Id-Usuario` | `200`, `401 sin_sesion`, `404` |

Ninguna respuesta incluye la contraseña ni su hash. Un email inexistente y una contraseña incorrecta responden igual y tardan lo mismo (se compara contra un hash de relleno), para no delatar qué emails tienen cuenta. El email se guarda en minúsculas y es único en MySQL.

**Sesión en el frontend: header `Authorization: Bearer <token>`.** El frontend guarda el token en el navegador y lo manda en cada solicitud. El gateway lo verifica (firma HS256, `exp` obligatorio) y:

| Caso | Respuesta del gateway |
|---|---|
| Sin `Authorization` | Pasa sin headers de identidad; el servicio decide si la ruta necesita sesión (`401 sin_sesion`) |
| Token válido | Pasa con `X-Id-Usuario`, `X-Email-Usuario` y `X-Rol-Usuario` del token |
| Token vencido | `401 sesion_vencida`; el frontend manda a ingresar |
| Token inválido (firma, algoritmo, formato) | `401 sin_sesion` |

La otra opción era una cookie `HttpOnly`: JavaScript no la puede leer, pero hay que protegerse de CSRF y cambia la respuesta del inicio de sesión. Con el header no hay CSRF, a cambio de que un script inyectado en la página pueda leer el token.

## Alternativas consideradas

| Opción | Cómo funciona | A favor | En contra |
|---|---|---|---|
| **A. JWT firmado con un secreto compartido (HS256)** | `usuarios` firma un token con los datos del usuario y el gateway verifica la firma con el mismo secreto. | El gateway no le pregunta nada a `usuarios` en cada solicitud: si `usuarios` se cae, las sesiones abiertas siguen andando. Es lo más simple de implementar. | El secreto está en dos servicios, y con él se pueden fabricar tokens. Un token no se puede revocar antes de que venza, así que la vida tiene que ser corta. Suma una dependencia. |
| **B. JWT firmado con un par de claves (RS256 o EdDSA)** | Igual que A, pero `usuarios` firma con la clave privada y el gateway solo tiene la pública. | Si se filtra la configuración del gateway, no se pueden fabricar tokens. | Hay que generar y guardar un par de claves en el `.env`. Mismo problema de revocación y misma dependencia que A. |
| **C. Sesión guardada en `usuarios`** | `usuarios` guarda la sesión y le da al cliente un identificador opaco, que el gateway le consulta a `usuarios` en cada solicitud. | Cerrar sesión o bloquear un usuario surte efecto al instante. No suma dependencias. | Una llamada más por solicitud, y si `usuarios` se cae nadie puede usar el sistema: pasa a ser un punto único de falla para todo, contra RNF4. |

Para el hash, **argon2id** resiste mejor el crackeo con GPU y es lo que recomienda OWASP hoy, pero hay que elegir memoria, iteraciones y formato de guardado; bcrypt no tiene parámetros que ajustar. Para el vendedor, **registrarse con un código secreto** permitía varios vendedores, pero suma un camino más que proteger.

## Consecuencias

- Si `usuarios` se cae, las sesiones abiertas siguen andando hasta que vencen; solo fallan el registro y el inicio de sesión.
- Cerrar sesión es borrar el token en el frontend: el token sigue siendo válido hasta que vence, como mucho 1 hora.
- `JWT_SECRETO` está en `usuarios` y en el gateway. Si se filtra, se pueden fabricar tokens de cualquier usuario, incluido el vendedor: hay que cambiarlo, y eso cierra todas las sesiones.
- Cambiar `VENDEDOR_CLAVE` en el `.env` no cambia la contraseña de una cuenta ya creada: hay que borrar el volumen de MySQL de `usuarios` o cambiarla en la base.
- Registrarse e iniciar sesión cuestan unos 50 a 100 ms de CPU por bcrypt. Se mide con los tests de carga ([ADR-013](ADR-013-capacidad-y-costos.md)).
