FROM golang:1.27.3-bookworm AS build
WORKDIR /app
ENV GOWORK=off

COPY task/ ./task/
COPY pkg/ ./pkg/

WORKDIR /app/task
RUN CGO_ENABLED=0 go build -mod=readonly -o /usr/local/bin/task ./cmd/app

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /usr/local/bin/task /usr/local/bin/task
EXPOSE 8084 9094
CMD ["/usr/local/bin/task"]