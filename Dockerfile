FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/repo-chat-bot .

FROM alpine:3.20
RUN apk add --no-cache git ca-certificates
WORKDIR /app
COPY --from=build /out/repo-chat-bot /app/repo-chat-bot
ENV REPO_PATH=/app/repo
ENTRYPOINT ["/app/repo-chat-bot"]
