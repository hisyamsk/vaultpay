FROM golang:1.25-bookworm AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/vaultpay ./cmd/api


FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/vaultpay /app/vaultpay

EXPOSE 8080

ENTRYPOINT ["/app/vaultpay"]
