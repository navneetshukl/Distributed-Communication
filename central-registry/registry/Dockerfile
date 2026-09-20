FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./

RUN go mod download
COPY shared/ ./shared/
COPY registry/ ./registry/
RUN cd registry && go build -o registry-server main.go

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/registry/registry-server .
EXPOSE 8080
CMD ["./registry-server"]