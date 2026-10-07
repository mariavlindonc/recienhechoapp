# --- Etapa 1: compilación ---
# Imagen con el toolchain de Go en la misma versión que declara go.mod (1.27).
# Alpine la hace más liviana de descargar; esta etapa no llega a la imagen final.
FROM golang:1.27-alpine AS build

# Directorio de trabajo dentro del contenedor; los comandos siguientes corren acá.
WORKDIR /src

# Se copian primero solo los manifiestos de dependencias. Docker cachea cada capa:
# mientras go.mod y go.sum no cambien, la descarga de abajo no se repite aunque
# cambie el código, y los rebuilds son mucho más rápidos.
COPY go.mod go.sum ./

# Descarga las dependencias (Gin y las suyas) a la caché de módulos de la imagen.
RUN go mod download

# Recién ahora se copia el código fuente (lo que cambia seguido).
COPY . .

# CGO_ENABLED=0 genera un binario estático que no necesita libc, requisito para
# correr sobre distroless/static. -trimpath saca las rutas locales del binario
# (builds reproducibles) y -ldflags="-s -w" quita símbolos de depuración para
# achicarlo.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/servicio .

# --- Etapa 2: ejecución ---
# Imagen mínima sin shell ni gestor de paquetes: menos superficie de ataque y
# pocos MB. La variante :nonroot ejecuta el proceso con un usuario sin privilegios.
FROM gcr.io/distroless/static-debian12:nonroot

# Solo se trae el binario compilado; el código fuente y el toolchain quedan afuera.
COPY --from=build /out/servicio /servicio

# Proceso principal del contenedor. Forma exec (JSON) para que el binario sea el
# PID 1 y reciba directamente las señales de docker stop (apagado ordenado).
ENTRYPOINT ["/servicio"]
