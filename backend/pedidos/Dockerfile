FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN go build -o /out/servicio .

FROM alpine:3

COPY --from=build /out/servicio /servicio

ENTRYPOINT ["/servicio"]
