GO_MODULES := user onboarding task chat pkg integration-tests
.PHONY: deps verify fmt lint test vet tools-check
deps:
	@set -e; for module in $(GO_MODULES); do (cd $$module && GOWORK=off go mod download all); done
verify:
	@set -e; for module in $(GO_MODULES); do (cd $$module && GOWORK=off go mod verify); done
fmt:
	@set -e; for module in $(GO_MODULES); do (cd $$module && golangci-lint fmt --config ../.golangci.yaml); done
lint:
	@set -e; for module in $(GO_MODULES); do (cd $$module && golangci-lint run --config ../.golangci.yaml ./...); done
test:
	@set -e; for module in $(GO_MODULES); do (cd $$module && go test -race -count=1 ./...); done
vet:
	@set -e; for module in $(GO_MODULES); do (cd $$module && go vet ./...); done
tools-check:
	go version
	golangci-lint version
