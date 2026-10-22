FROM golang:1.27.3-alpine

WORKDIR /app

# 安装必要的工具
RUN apk add --no-cache git

# 复制go.mod和go.sum
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用。VERSION 必须是 X.Y.Z、X.Y.Z-tp.N 或 TokenPro-R<N>-vX.Y.Z，否则用 VERSION 文件。
ARG VERSION=
RUN VERSION_VALUE="$(./scripts/resolve-version.sh "${VERSION:-}")" && \
    go build -ldflags="-s -w -X main.Version=${VERSION_VALUE}" -o main ./cmd/server/

# 暴露端口
EXPOSE 8080

# 运行应用
CMD ["./main"]
