FROM golang:1.27.3-bookworm AS build
WORKDIR /app
ENV GOWORK=off

COPY go.mod go.sum ./
COPY controller ./controller
COPY entity ./entity
COPY repository ./repository
COPY usecase ./usecase
COPY cmd/auth ./cmd/auth

RUN CGO_ENABLED=0 go build -mod=mod -o /usr/local/bin/auth ./cmd/auth

FROM debian:bookworm-slim
COPY --from=build /usr/local/bin/auth /usr/local/bin/auth

EXPOSE 8080
CMD ["/usr/local/bin/auth"]