.PHONY: test

build: .FORCE
	go build ./...

test: .FORCE
	go test ./... -short

.FORCE:
