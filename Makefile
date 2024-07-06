.PHONY: test

test: .FORCE
	#go test ./... -run 'TestMention'
	go build ./...

.FORCE:
