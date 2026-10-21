# ADR-005: Canal de las notificaciones y servidor de envío

## Estado

Aceptado (Entrega 2)

## Contexto

La capacidad que publicamos es **notificaciones** ([ADR-004](ADR-004-contrato-de-notificaciones.md)). El enunciado no impone el canal: pide una capacidad publicada que otro grupo pueda consumir. Los documentos de la Entrega 1 (`docs/SPEC.md` C9, el contrato 1.0.0 con `canal: email`) asumieron email como punto de partida, y el `enum` de `canal` deja abierta la puerta a otros.

Ningún canal de entrega real es a la vez gratis y confiable: los proveedores con plan gratuito tienen cupos y pueden mandar a spam o bloquear la cuenta, y un SMTP real obliga a guardar credenciales de un tercero. El diseño ya asume que el envío puede fallar (estado `fallida`, reintentos con espera, `POST` que responde `202` sin esperar al servidor de correo), así que la falta de confiabilidad del proveedor no tumba el sistema.

Hay que decidir contra qué servidor de correo habla `notificaciones` en desarrollo, en los tests y en la demo.

## Decisión

- **Canal:** email, como dice el contrato 1.0.0.
- **Protocolo:** SMTP genérico, con `net/smtp` de la librería estándar de Go. Servidor, puerto, usuario, clave y remitente salen de variables de entorno (`SMTP_HOST`, `SMTP_PUERTO`, `SMTP_USUARIO`, `SMTP_CLAVE`, `SMTP_REMITENTE`): cambiar de servidor no toca código.
- **Desarrollo y tests: Mailpit** (`axllent/mailpit`), un buzón falso que atrapa los mails y los muestra en una web. Es el valor por defecto del `docker-compose` y de `.env.example`.
- **Demo con entrega real: Gmail** (`smtp.gmail.com:587`, STARTTLS) con una cuenta creada para el proyecto y una **contraseña de aplicación** (requiere verificación en 2 pasos). Para usarlo se cambian las variables `SMTP_*` en el `.env` de quien hace la demo. La contraseña nunca entra al repositorio.
- Los tests unitarios no usan ningún servidor: prueban el envío contra un remitente falso (el puerto `Remitente` del servicio).

## Alternativas consideradas

| Opción | A favor | En contra |
|---|---|---|
| **Mailpit + Gmail (elegida)** | Desarrollo y tests sin internet, cuentas ni cupos, y sin mandar mails de prueba a gente real; la web de Mailpit sirve de evidencia; apagarlo permite demostrar la caída. Con Gmail hay entrega real cuando hace falta, con el mismo código | Hay que mantener dos configuraciones; la entrega real solo se prueba en la demo |
| **Solo Mailpit** | Lo más simple y determinístico | No entrega nada de verdad: no prueba la llegada a una casilla ni el comportamiento ante spam |
| **Solo Gmail** | Entrega real siempre | Cada prueba manda mails reales; límite de unos 500 por día; depende de internet y de una cuenta personal que Google puede bloquear |
| **Proveedor con plan gratis (Resend, Brevo, etc.)** | Entrega real, pensado para envíos de sistemas | Alta de cuenta y dominio, cuotas, otra credencial más |
| **Otro canal (SMS, WhatsApp)** | Más cerca del usuario | Siempre requiere proveedor con costo o aprobación; complica probar localmente |

## Consecuencias

- Los tests y el desarrollo no dependen de internet ni de terceros.
- La indisponibilidad del servidor de correo se puede provocar apagando el contenedor de Mailpit y usarla como caída controlada (D10): las notificaciones quedan `pendiente`, se reintentan y, si no vuelve, terminan `fallida`.
- Gmail limita a unos 500 mails por día: alcanza para la demo, pero es el primer límite de capacidad del servicio (se analiza en D13).
- Mailpit se suma al `docker-compose` junto con el servicio real de notificaciones.
