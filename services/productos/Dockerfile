# --- Etapa 1: compilación ---
# Imagen con el toolchain de Go en la misma versión que declara go.mod (1.27).
# Esta etapa no llega a la imagen final.
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

# Compila el servicio a un único binario.
RUN go build -o /out/servicio .

# --- Etapa 2: ejecución ---
# Alpine es chica (unos pocos MB) y trae shell, así se puede entrar al contenedor
# a depurar con `docker exec -it <contenedor> sh`.
FROM alpine:3

# Solo se trae el binario compilado; el código fuente y el toolchain quedan afuera.
COPY --from=build /out/servicio /servicio

# Proceso principal del contenedor. Forma exec (JSON) para que el binario sea el
# PID 1 y reciba directamente las señales de docker stop (apagado ordenado).
ENTRYPOINT ["/servicio"]
