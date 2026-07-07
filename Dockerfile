# syntax=docker/dockerfile:1

FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.23.4-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/athlete-log ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/migrate ./cmd/migrate

FROM alpine:3.21
RUN apk add --no-cache ca-certificates && addgroup -S app && adduser -S app -G app
WORKDIR /app
COPY --from=backend /out/athlete-log /usr/local/bin/athlete-log
COPY --from=backend /out/migrate /usr/local/bin/athlete-log-migrate
COPY --from=backend /src/frontend/dist /app/frontend/dist
COPY scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh && mkdir -p /data && chown -R app:app /data /app
USER app
ENV DATA_DIR=/data
ENV TOKEN_FILE=/data/bearer.token
ENV STATIC_DIR=/app/frontend/dist
ENV APP_ADDR=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["docker-entrypoint.sh"]
