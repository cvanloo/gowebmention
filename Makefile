.PHONY: test

test: .FORCE
	#go test ./... -run 'TestMention'
	#go test ./... -run 'TestReceive'
	go build cmd/sender/main.go

.FORCE:
