FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o frontend .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/frontend .
COPY static ./static
COPY templates ./templates

RUN adduser -D -H appuser
USER appuser

EXPOSE 8080
CMD ["./frontend"]
