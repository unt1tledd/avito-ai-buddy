FROM golang:1.27.0 AS build
WORKDIR /app
ENV GOWORK=off

COPY onboarding/ ./onboarding/
COPY pkg/ ./pkg/

WORKDIR /app/onboarding
RUN CGO_ENABLED=0 go build -mod=readonly -o /usr/local/bin/onboarding ./cmd/app

FROM debian:bookworm-slim
COPY --from=build /usr/local/bin/onboarding /usr/local/bin/onboarding
EXPOSE 8080 50051
CMD ["/usr/local/bin/onboarding"]