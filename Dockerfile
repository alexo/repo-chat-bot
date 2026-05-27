FROM golang:1.25-bookworm AS build
ARG VERSION=dev
ARG RELEASE_DATE=unknown
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
  -ldflags="-s -w -X main.Version=${VERSION} -X main.ReleaseDate=${RELEASE_DATE}" \
  -o /out/repo-chat-bot .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/repo-chat-bot /app/repo-chat-bot
ENV REPO_PATH=/app/repo
ENTRYPOINT ["/app/repo-chat-bot"]
