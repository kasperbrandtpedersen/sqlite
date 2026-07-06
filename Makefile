$(shell test -f .env || cp .env.example .env)

include .env
export

.PHONEY: build
build:
	@go build

.PHONEY: test
test:
	@go test -v ./...

.PHONEY: dep
dep:
	@go mod tidy