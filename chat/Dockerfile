FROM golang:1.27.3-bookworm AS build
WORKDIR /app
ENV GOWORK=off

COPY chat/ ./chat/
COPY pkg/ ./pkg/

WORKDIR /app/chat
RUN CGO_ENABLED=1 go build -mod=readonly -o /usr/local/bin/chat ./cmd/app

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /usr/local/bin/chat /usr/local/bin/chat
EXPOSE 8080 50051
CMD ["/usr/local/bin/chat"]