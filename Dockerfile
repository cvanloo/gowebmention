FROM golang:1.23.4-alpine3.20 AS build
WORKDIR /usr/src/mentionee
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN go build -v -o /usr/local/bin/mentionee cmd/mentionee/main.go
CMD ["mentionee"]
