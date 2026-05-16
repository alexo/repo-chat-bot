FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/repo-chat-bot .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/repo-chat-bot /app/repo-chat-bot
ENV REPO_PATH=/app/repo
ENTRYPOINT ["/app/repo-chat-bot"]
