FROM golang:1.27.0
WORKDIR /app
ENV GOWORK=off

COPY go.mod go.sum ./
COPY controller ./controller
COPY entity ./entity
COPY repository ./repository
COPY usecase ./usecase
COPY cmd/auth ./cmd/auth

RUN go build -mod=mod -o /usr/local/bin/auth ./cmd/auth

EXPOSE 8080
CMD ["/usr/local/bin/auth"]