FROM m.daocloud.io/docker.io/library/node:22-alpine AS frontend-build
WORKDIR /workspace/frontend

COPY frontend/package*.json ./
RUN npm ci
COPY frontend ./
RUN npm run build
RUN apk add --no-cache brotli && \
    find dist -type f \( -name '*.html' -o -name '*.js' -o -name '*.css' -o -name '*.svg' -o -name '*.json' -o -name '*.txt' -o -name '*.xml' -o -name '*.map' \) | while read -r file; do \
      brotli -f -q 11 "$file" -o "$file.br"; \
      gzip -f -k -9 "$file"; \
    done

FROM m.daocloud.io/docker.io/library/golang:1.24-alpine AS backend-build
WORKDIR /workspace/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/blog-server ./cmd/server

FROM m.daocloud.io/docker.io/library/alpine:3.20
WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=backend-build /out/blog-server /app/blog-server
COPY --from=frontend-build /workspace/frontend/dist /app/web
COPY config.toml /app/config.toml

RUN mkdir -p /app/data /app/logs

ENV PORT=20260
ENV APP_ROOT=/app
ENV CONFIG_PATH=/app/config.toml
ENV DATA_DIR=/app/data
ENV LOG_DIR=/app/logs
ENV WEB_DIR=/app/web
ENV TZ=Asia/Shanghai

EXPOSE 20260

CMD ["/app/blog-server"]
