FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY shared/ ./shared/
COPY node/ ./node/
RUN cd node && go mod init node && go get github.com/gorilla/websocket && go build -o node-server main.go hub.go registry_client.go router.go ws_handler.go http_handler.go

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/node/node-server .
EXPOSE 3000
CMD ["./node-server"]