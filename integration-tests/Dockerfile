FROM golang:1.27.3-bookworm
WORKDIR /src
COPY pkg ./pkg
COPY integration-tests ./integration-tests
WORKDIR /src/integration-tests
ENV GOWORK=off
RUN go mod download
CMD ["go", "test", "-tags=integration", "-race", "-count=1", "-timeout=10m", "-v", "./..."]
