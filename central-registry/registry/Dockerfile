FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY shared/ ./shared/
COPY registry/ ./registry/
RUN cd registry && go build -o registry-server main.go server.go store.go

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/registry/registry-server .
EXPOSE 8080
CMD ["./registry-server"]