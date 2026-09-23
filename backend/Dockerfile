FROM golang:1.25.11-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /out/server ./cmd/server && \
    go build -o /out/migrate ./cmd/migrate && \
    go build -o /out/bootstrap-superadmin ./cmd/bootstrap-superadmin

FROM alpine:3.22

WORKDIR /app

RUN addgroup -S app && adduser -S app -G app

COPY --from=build /out/server /usr/local/bin/audio-speech-vault-server
COPY --from=build /out/migrate /usr/local/bin/audio-speech-vault-migrate
COPY --from=build /out/bootstrap-superadmin /usr/local/bin/audio-speech-vault-bootstrap-superadmin
COPY migrations ./migrations
COPY scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN chmod +x /usr/local/bin/docker-entrypoint.sh && \
    chown -R app:app /app

USER app

EXPOSE 8080

ENTRYPOINT ["docker-entrypoint.sh"]
