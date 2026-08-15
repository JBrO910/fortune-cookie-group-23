FROM golang:1.25.12-alpine3.24 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o frontend . \
    && mkdir /out \
    && cp frontend /out/ \
    && cp -r static templates /out/

FROM alpine:3.24
WORKDIR /app

COPY --from=builder /out/ ./

RUN adduser -D -H -u 1000 appuser
USER appuser

EXPOSE 8080
CMD ["./frontend"]
