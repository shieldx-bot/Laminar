ARG GO_VERSION=1.24.11
FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the root module (main package) into a static binary.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ws ./

FROM alpine:3.20

# Needed for HTTPS clients and some Go libs that rely on system CAs.
RUN apk add --no-cache ca-certificates && update-ca-certificates \
    && addgroup -S app && adduser -S -G app app

WORKDIR /app
COPY --from=build /out/ws ./ws

USER app
EXPOSE 8888
CMD ["./ws"]