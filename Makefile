.PHONY: test

test: .FORCE
	go test ./... -run 'TestMention'

.FORCE:
