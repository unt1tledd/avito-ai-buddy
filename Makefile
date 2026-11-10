GO_MODULES := auth user onboarding task chat pkg integration-tests
GO_PACKAGES := $(foreach module,$(GO_MODULES),./$(module)/...)
.PHONY: deps verify fmt lint test vet tools-check
deps:
	@set -e; for module in $(GO_MODULES); do (cd $$module && GOWORK=off go mod download all); done
verify:
	@set -e; for module in $(GO_MODULES); do (cd $$module && GOWORK=off go mod verify); done
fmt:
	@set -e; for module in $(GO_MODULES); do (cd $$module && golangci-lint fmt --config ../.golangci.yaml); done
lint:
	@set -e; for module in $(GO_MODULES); do ( \
		cd $$module; \
		packages=$$(go list ./...); \
		if [ -n "$$packages" ]; then golangci-lint run --config ../.golangci.yaml ./...; fi \
	); done
test:
	@set -e; for module in $(GO_MODULES); do ( \
		cd $$module; \
		packages=$$(go list ./...); \
		if [ -n "$$packages" ]; then go test -race -count=1 ./...; fi \
	); done
vet:
	go vet $(GO_PACKAGES)
tools-check:
	go version
	golangci-lint version
