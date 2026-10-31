# 多阶段构建：前端 → Go embed → 运行镜像（arm64/amd64 通用，无 CGO）
# 镜像可通过 build-arg 覆盖前缀（国内镜像加速）：--build-arg NODE_IMAGE=docker.m.daocloud.io/library/node:22-alpine
ARG NODE_IMAGE=node:22-alpine
ARG GO_IMAGE=golang:1.27-alpine
ARG RUNTIME_IMAGE=alpine:3.20

FROM ${NODE_IMAGE} AS web
WORKDIR /app/web
COPY web/package.json ./
RUN npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM ${GO_IMAGE} AS build
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /app/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /hookdock .

FROM ${RUNTIME_IMAGE}
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 1000 hookdock
COPY --from=build /hookdock /usr/local/bin/hookdock
USER hookdock
WORKDIR /data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/hookdock"]
