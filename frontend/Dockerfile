FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o frontend .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/frontend .
COPY static ./static
COPY templates ./templates

EXPOSE 8080
CMD ["./frontend"]
