FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod .
COPY main.go .
RUN go build -o imgcompress .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/imgcompress .
EXPOSE 8080
CMD ["./imgcompress"]
