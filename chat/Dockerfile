FROM golang:1.27.0 AS build
WORKDIR /app
ENV GOWORK=off

COPY chat/ ./chat/
COPY pkg/ ./pkg/

WORKDIR /app/chat
RUN CGO_ENABLED=0 go build -mod=readonly -o /usr/local/bin/chat ./cmd/app

FROM debian:bookworm-slim
COPY --from=build /usr/local/bin/chat /usr/local/bin/chat
EXPOSE 8080 50051
CMD ["/usr/local/bin/chat"]