FROM golang:1.27.3-bookworm AS build
WORKDIR /app
ENV GOWORK=off

COPY user/go.mod user/go.sum ./user/
COPY user/cmd/app ./user/cmd/app
COPY user/internal ./user/internal
COPY user/migrations ./user/migrations
COPY pkg/go.mod pkg/go.sum ./pkg/
COPY pkg/generated/user/v1 ./pkg/generated/user/v1
COPY pkg/health ./pkg/health

WORKDIR /app/user
RUN CGO_ENABLED=0 go build -mod=mod -o /usr/local/bin/user ./cmd/app

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /usr/local/bin/user /usr/local/bin/user

ENV GRPC_HOST=0.0.0.0
EXPOSE 8080 9090
CMD ["/usr/local/bin/user"]
