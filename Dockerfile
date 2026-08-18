FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/telegram-tracker ./cmd/bot

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 1000 bot
WORKDIR /app
COPY --from=build /out/telegram-tracker /app/telegram-tracker
USER bot
ENTRYPOINT ["/app/telegram-tracker"]
