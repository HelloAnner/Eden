FROM m.daocloud.io/docker.io/library/node:22-alpine AS frontend-build
WORKDIR /workspace/frontend

COPY frontend/package*.json ./
RUN npm ci
COPY frontend ./
RUN npm run build

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

RUN mkdir -p /app/data

ENV PORT=20260
ENV APP_ROOT=/app
ENV CONFIG_PATH=/app/config.toml
ENV DATA_DIR=/app/data
ENV WEB_DIR=/app/web
ENV TZ=Asia/Shanghai

EXPOSE 20260

CMD ["/app/blog-server"]
