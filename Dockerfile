FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /app/server ./cmd/server

FROM alpine:3.19

WORKDIR /app

RUN addgroup -S app && adduser -S -G app app

COPY --from=builder /app/server /app/server

USER app:app

EXPOSE 8080

CMD ["/app/server"]
