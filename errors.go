package webmention

import "errors"

var (
	ErrNotImplemented       = errors.New("not implemented")
	ErrNoEndpointFound      = errors.New("no webmention endpoint found")
	ErrNoRelWebmention      = errors.New("no webmention relationship found")
	ErrInvalidRelWebmention = errors.New("target has invalid webmention url")
)
