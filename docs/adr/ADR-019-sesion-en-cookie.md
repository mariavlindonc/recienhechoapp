# ADR-019: Autenticación con la sesión en una cookie HttpOnly

## Estado

Aceptado por María el 06/11/2026. Reemplaza a [ADR-016](ADR-016-autenticacion.md).

## Contexto

[ADR-016](ADR-016-autenticacion.md) decidió que el frontend guarda el JWT en el navegador y lo manda en `Authorization: Bearer`. Eso tenía un costo aceptado: un script inyectado en la página podía leer el token.

El seguimiento en vivo ([ADR-018](ADR-018-tiempo-real.md)) agregó un problema: el navegador no deja mandar `Authorization` al abrir un WebSocket, así que el token tenía que viajar en la URL. Las URLs suelen quedar en logs que no controlamos, como los de un balanceador en la nube.

Un parche solo para el WebSocket resolvía la URL pero no el otro riesgo, y dejaba dos formas distintas de autenticar. Este ADR resuelve las dos cosas con un solo mecanismo. La cookie era la alternativa descartada en ADR-016 por el riesgo de CSRF y porque cambiaba el inicio de sesión. Con el WebSocket, esos costos valen la pena, y el riesgo de CSRF se cubre con `SameSite`.

Todo lo que ADR-016 decidió y no se menciona como cambio sigue igual, y queda repetido acá para que este ADR se pueda leer solo.

## Decisión

**La sesión del navegador viaja en una cookie que JavaScript no puede leer:**

```
Set-Cookie: sesion=<token>; Path=/; HttpOnly; Secure; SameSite=Strict
```

- `HttpOnly`: un script inyectado no la puede leer.
- `Secure`: solo viaja por HTTPS. `localhost` cuenta como seguro, así que funciona en desarrollo.
- `SameSite=Strict`: el navegador no la manda en solicitudes que vienen de otro sitio. Es lo que frena el CSRF.
- Sin vencimiento propio: se borra al cerrar el navegador, y el token igual vence a la hora.

`usuarios` la deja al iniciar sesión, y el navegador la manda sola en cada solicitud y al abrir el WebSocket. El frontend ya no guarda ni maneja el token: en `sessionStorage` quedan solo los datos del usuario para las pantallas.

**El api-gateway lee la identidad de la cookie**, y la saca antes de reenviar: los servicios reciben `X-Id-Usuario`, `X-Email-Usuario` y `X-Rol-Usuario`, nunca el token.

| Caso | Respuesta del gateway |
|---|---|
| Sin cookie ni `Authorization` | Pasa sin headers de identidad; el servicio decide si la ruta necesita sesión (`401 sin_sesion`) |
| Cookie válida | Pasa con la identidad del token |
| Cookie vencida o inválida | Pasa sin identidad, como si no estuviera. Si se rechazara, tampoco se podría volver a ingresar, porque el `POST` de inicio de sesión también la lleva |
| `Authorization: Bearer` válido | Pasa con la identidad del token. Tiene prioridad sobre la cookie |
| `Authorization: Bearer` vencido | `401 sesion_vencida` |
| `Authorization: Bearer` inválido (firma, algoritmo, formato) | `401 sin_sesion` |

**`Authorization` sigue valiendo para los clientes que no son navegadores**, como los tests de carga y `app.sh`. Por eso `POST /usuarios/sesion` sigue devolviendo el token en el cuerpo, aunque el frontend no lo use.

**WebSocket y origen.** Con la sesión en una cookie importa desde qué sitio se abre el WebSocket. Además de `SameSite`, el servidor del frontend (`servidor.ts`) rechaza con `403` un `Origin` distinto de su host, antes de abrir el puente. En desarrollo, el proxy de Vite le saca el `Origin` a la solicitud, porque `pedidos` rechaza uno distinto de su host.

**Un token en la URL no identifica a nadie**, en ninguna ruta.

**Cerrar sesión es una ruta:** `DELETE /usuarios/sesion` borra la cookie, porque JavaScript no puede hacerlo. El token sigue siendo válido hasta que vence, pero el navegador ya no lo tiene.

### Lo que sigue igual que en ADR-016

**JWT firmado con un secreto compartido (HS256), con una vida de 1 hora.** `usuarios` lo firma al iniciar sesión y el api-gateway verifica la firma con el mismo secreto (`JWT_SECRETO`, de al menos 32 bytes), solo con HS256 y con `exp` obligatorio. El gateway borra los headers de identidad que mande el cliente. El token lleva `sub` (id del usuario), `email`, `rol`, `iat` y `exp`. Dependencia: `github.com/golang-jwt/jwt/v5`.

**Contraseñas con bcrypt** (`golang.org/x/crypto/bcrypt`, costo por defecto, 10), validadas entre 8 y 72 bytes, porque bcrypt ignora lo que pasa de 72.

**Vendedor desde el `.env`.** El registro público crea solo compradores. Al arrancar, `usuarios` crea la cuenta del vendedor con `VENDEDOR_EMAIL` y `VENDEDOR_CLAVE` si no existe; si existe, no la toca.

**Rutas de `usuarios`:**

| Ruta | Qué hace | Respuestas |
|---|---|---|
| `POST /usuarios` | Registro de un comprador (`email`, `nombre`, `clave`) | `201`, `400 solicitud_invalida`, `409 email_repetido` |
| `POST /usuarios/sesion` | Inicio de sesión (`email`, `clave`); deja la cookie y devuelve `token`, `venceEn` y el usuario | `200`, `401 credenciales_invalidas` |
| `DELETE /usuarios/sesion` | Cierre de sesión: borra la cookie | `204` |
| `GET /usuarios/yo` | Perfil del usuario de `X-Id-Usuario` | `200`, `401 sin_sesion`, `404` |

Ninguna respuesta incluye la contraseña ni su hash. Un email inexistente y una contraseña incorrecta responden igual y tardan lo mismo (se compara contra un hash de relleno), para no delatar qué emails tienen cuenta. El email se guarda en minúsculas y es único en MySQL.

**Límite de intentos.** El api-gateway deja pasar hasta `USUARIOS_INTENTOS_POR_MINUTO` (10 por defecto) `POST /usuarios` y `POST /usuarios/sesion` por minuto desde cada IP, y después responde `429 demasiados_intentos` con `Retry-After`. Los navegadores entran por el servidor del frontend, así que `servidor.ts` le informa al gateway la IP real en `X-IP-Navegador`, y pisa lo que haya mandado el cliente. Como el gateway publica su puerto, le cree solo si viene `X-Clave-Frontend` con la clave compartida (`CLAVE_FRONTEND`); si no, cuenta la IP de la conexión. No se usa `X-Forwarded-For`, que cualquiera puede inventar. El gateway saca esos dos headers antes de reenviar (ver las limitaciones en [ARCHITECTURE](../ARCHITECTURE.md#limitaciones-conocidas-y-deuda-técnica)).

## Alternativas consideradas

Cómo viaja la sesión:

| Opción | A favor | En contra |
|---|---|---|
| **Cookie `HttpOnly` en todo el sistema** (elegida) | Un script inyectado no puede leer el token. Un solo mecanismo para HTTP y WebSocket. Nada en la URL | Hay que cuidar el CSRF y el origen del WebSocket, y cerrar sesión pasa a ser una ruta |
| `Authorization` en HTTP y `Sec-WebSocket-Protocol` en WebSocket | Cambio chico, y sin CSRF | Un script inyectado puede seguir leyendo el token. Dos mecanismos para lo mismo |
| Ticket de un solo uso para el WebSocket | Lo que quede en un log ya no sirve | Suma una ruta y guardar los tickets, y no resuelve el token en JavaScript |
| Token en la URL del WebSocket, como decía ADR-018 | Lo más simple | El token puede quedar en logs fuera de nuestro control |

Las alternativas sobre el tipo de token (claves asimétricas, sesiones guardadas en `usuarios`), el hash (argon2id) y el vendedor siguen siendo las de [ADR-016](ADR-016-autenticacion.md).

## Consecuencias

- Un script inyectado en la página ya no puede robar el token, aunque sí puede hacer solicitudes mientras la página esté abierta.
- **El despliegue tiene que usar HTTPS:** fuera de `localhost`, con `Secure` el navegador no guarda la cookie por HTTP. Además, el frontend y el gateway tienen que verse como un solo sitio, como hoy, con `/api` en el servidor del frontend.
- Los servicios ya no pueden ver el token, porque el gateway saca la cookie.
- Cerrar la pestaña borra los datos de `sessionStorage`, pero la cookie queda hasta cerrar el navegador: una pestaña nueva pide ingresar aunque la cookie siga ahí.
- Si `usuarios` se cae, las sesiones abiertas siguen andando hasta que vencen; solo fallan el registro, el inicio y el cierre de sesión. Si no se puede cerrar sesión, la pantalla cierra igual y la cookie se va al cerrar el navegador.
- `JWT_SECRETO` está en `usuarios` y en el gateway. Si se filtra, se pueden fabricar tokens de cualquier usuario, incluido el vendedor: hay que cambiarlo, y eso cierra todas las sesiones.
- Cambiar `VENDEDOR_CLAVE` en el `.env` no cambia la contraseña de una cuenta ya creada.
- Registrarse e iniciar sesión cuestan unos 50 a 100 ms de CPU por bcrypt ([ADR-013](ADR-013-capacidad-y-costos.md)).

## Validación (06/11/2026)

Probado en el navegador con la app completa en Docker:

- La cookie queda con `HttpOnly`, `Secure`, `SameSite=Strict` y sin vencimiento. `document.cookie` no la muestra, y en `sessionStorage` no hay token.
- Con una cookie vieja e inválida en el navegador, el inicio de sesión funciona.
- El WebSocket se abre con la cookie, y un `Origin` de otro sitio se rechaza aunque la solicitud lleve una cookie válida.
- Cerrar sesión borra la cookie.
- El token no aparece en los logs del api-gateway, de `pedidos` ni del servidor del frontend.
- `app.sh` sigue entrando con `Authorization`. Los tests de carga usan lo mismo, pero no se volvieron a correr.
