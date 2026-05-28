FROM golang:1.25-bookworm AS build
ARG VERSION=dev
ARG RELEASE_DATE=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
  -ldflags="-s -w -X main.Version=${VERSION} -X main.ReleaseDate=${RELEASE_DATE}" \
  -o /out/repo-chat-bot .

# Distroless static: ~2 MB base, ships ca-certificates + tzdata, default user
# is `nonroot` (uid 65532). No shell, no apt, no git — minimal attack surface.
# The bot's only runtime dependency is HTTPS, which ca-certificates covers.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/repo-chat-bot /app/repo-chat-bot
ENV REPO_PATH=/app/repo
ENTRYPOINT ["/app/repo-chat-bot"]
