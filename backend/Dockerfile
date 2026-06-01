# syntax=docker/dockerfile:1.7
FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# GIT_SHA stamps internal/build.Version so GET /health reports the deployed
# commit (#310). Defaults to "dev" for builds that don't pass the arg.
ARG GIT_SHA=dev
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
	-ldflags="-s -w -X github.com/gregwym/offbook/backend/internal/build.Version=${GIT_SHA}" \
	-o /out/server ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/server /app/server
COPY migrations /app/migrations
ENV PORT=8000
USER app
EXPOSE 8000
ENTRYPOINT ["/app/server"]
