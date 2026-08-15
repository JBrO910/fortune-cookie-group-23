FROM golang:1.25.12-alpine3.24 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o backend .

FROM alpine:3.24
WORKDIR /app

COPY --from=builder /app/backend .

RUN adduser -D -H -u 1000 appuser
USER appuser

EXPOSE 9000
CMD ["./backend"]
