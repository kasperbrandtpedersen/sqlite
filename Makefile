include .env
export

.PHONEY: build
build: gen
	@go build

.PHONEY: test
test:
	@go test -v ./...

.PHONEY: dep
dep:
	@go mod tidy