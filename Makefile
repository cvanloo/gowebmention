.PHONY: test

test: .FORCE
	go test ./...

.FORCE:
