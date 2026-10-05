# FROM golang:1.25.0

# WORKDIR /app

# COPY go.mod go.sum ./

# RUN go mod download

# COPY . .

# RUN go build -o url-shortener ./cmd/server

# EXPOSE 8080

# CMD ["./url-shortener"]

# Stage 1: Build
FROM golang:1.25.0 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -o url-shortener ./cmd/server


# # Stage 2: Run
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/url-shortener .

EXPOSE 8080

CMD ["./url-shortener"]