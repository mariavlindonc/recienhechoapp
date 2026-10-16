# ADR-005: Canal de las notificaciones y servidor de envío

## Estado

Propuesto

## Contexto

La capacidad que publicamos es **notificaciones** ([ADR-004](ADR-004-contrato-de-notificaciones.md)). El canal por el que se envían no está decidido: el enunciado no lo impone, pide una capacidad publicada que otro grupo pueda consumir. Los documentos de la Entrega 1 (`docs/SPEC.md` C9, el contrato 1.0.0 con `canal: email`) asumieron email como punto de partida, pero no se discutió como decisión, y el `enum` de `canal` deja abierta la puerta a otros.

Ningún canal de entrega real es a la vez gratis y confiable: los proveedores con plan gratuito tienen cupos y pueden mandar a spam o bloquear la cuenta, y un SMTP real obliga a guardar credenciales de un tercero. El diseño ya asume que el envío puede fallar (estado `fallida`, reintentos con espera, `POST` que responde `202` sin esperar al servidor de correo), así que la falta de confiabilidad del proveedor no tumba el sistema.

Hay que decidir contra qué servidor de correo habla `notificaciones` en desarrollo, en los tests y en la demo.

## Decisión

**Pendiente.** Quedan abiertas tres cosas:

1. **Canal:** email (lo que dice hoy el contrato 1.0.0) u otro.
2. **Servidor de envío para desarrollo, tests y demo de la Entrega 2:** Mailpit o un SMTP real.
3. **Cómo y cuándo se muestra una entrega real**, si hace falta.

**Propuesta para discutir** (no es una decisión):

- Email por SMTP genérico, con servidor, puerto y remitente tomados de variables de entorno, de modo que cambiar de servidor no toque código.
- Mailpit (`axllent/mailpit`), un buzón falso que atrapa los mails y los muestra en una web, como servidor por defecto del compose, los tests y la demo.
- Documentar cómo apuntar `SMTP_*` a un proveedor real, y decidir para la presentación grupal si hace falta.

## Alternativas consideradas

| Opción | A favor | En contra |
|---|---|---|
| **Mailpit (propuesta)** | Gratis, local y determinístico; sin cuentas, cupos ni credenciales; no manda mails de prueba a gente real; la web sirve de evidencia; permite apagarlo para demostrar la caída | No entrega nada de verdad: no prueba la entrega a una casilla ni el comportamiento ante spam |
| **SMTP de un proveedor con plan gratis** | Entrega real; el mismo código | Cuotas diarias, posible bloqueo o spam, credenciales fuera del repo, depende de un tercero |
| **Gmail con contraseña de aplicación** | Sin alta de proveedor nuevo | Límites de envío, pensado para uso personal, riesgo para la cuenta |
| **Otro canal (SMS, WhatsApp)** | Más cerca del usuario | Siempre requiere proveedor con costo o aprobación; complica probar localmente |
| **Proveedor de pago** | Mejor confiabilidad y métricas de entrega | Costo; fuera del alcance de esta entrega (se estima en D13) |

## Consecuencias

Si se adopta la propuesta:

- Los tests de integración y la demo no dependen de internet ni de terceros.
- No hay evidencia de entrega real hasta que se conecte un SMTP real; si la presentación la necesita, se cambia la configuración, no el código.
- La indisponibilidad del servidor de correo se puede provocar apagando el contenedor y usar como caída controlada ([D10](ADR-003-comunicacion-entre-servicios.md)), con las notificaciones quedando `pendiente` y reintentándose hasta quedar `fallida`.
- Mailpit se suma al `docker-compose` y a la tabla de infraestructura de `AGENTS.md`.
- Si se pasa a un proveedor real, las credenciales van en variables de entorno, fuera del repositorio, y se registra el cambio en este ADR (o en uno que lo reemplace).
