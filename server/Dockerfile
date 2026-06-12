FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/rootaika-server ./cmd/rootaika-server

FROM alpine:3.21

RUN adduser -D -H rootaika
WORKDIR /data
COPY --from=build /out/rootaika-server /usr/local/bin/rootaika-server

ENV ROOTAIKA_ADDR=:8080
ENV ROOTAIKA_DB_PATH=/data/rootaika.db

EXPOSE 8080
VOLUME ["/data"]
USER rootaika

ENTRYPOINT ["rootaika-server"]
