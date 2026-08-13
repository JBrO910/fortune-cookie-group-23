FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o backend .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/backend .

RUN adduser -D -H appuser
USER appuser

EXPOSE 9000
CMD ["./backend"]
