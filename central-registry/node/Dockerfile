FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./

RUN go mod download
COPY shared/ ./shared/
COPY node/ ./node/
RUN cd node && go build -o node-server main.go

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/node/node-server .
EXPOSE 3000
CMD ["./node-server"]