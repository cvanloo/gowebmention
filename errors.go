package webmention

import "errors"

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrNoEndpointFound = errors.New("no webmention endpoint found")
)