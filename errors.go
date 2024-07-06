package webmention

import (
	"fmt"
	"errors"
	"net/http"
)

var (
	ErrNotImplemented       = errors.New("not implemented")
	ErrNoEndpointFound      = errors.New("no webmention endpoint found")
	ErrNoRelWebmention      = errors.New("no webmention relationship found")
	ErrInvalidRelWebmention = errors.New("target has invalid webmention url")
)

type (
	ErrorResponder interface {
		RespondError(w http.ResponseWriter, r *http.Request) bool
	}

	ErrMethodNotAllowed struct{}

	ErrBadRequest struct {
		Message string
	}

	ErrTooManyRequests struct{}
)

func MethodNotAllowed() error {
	return ErrMethodNotAllowed{}
}

func (e ErrMethodNotAllowed) Error() string {
	return "method not allowed"
}

func (e ErrMethodNotAllowed) RespondError(w http.ResponseWriter, r *http.Request) bool {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return true
}

func BadRequest(msg string) error {
	return ErrBadRequest{msg}
}

func (e ErrBadRequest) Error() string {
	return fmt.Sprintf("bad request: %s", e.Message)
}

func (e ErrBadRequest) RespondError(w http.ResponseWriter, r *http.Request) bool {
	http.Error(w, e.Error(), http.StatusBadRequest)
	return true
}

func TooManyRequests() error {
	return ErrTooManyRequests{}
}

func (e ErrTooManyRequests) Error() string {
	return "too many requests"
}

func (e ErrTooManyRequests) RespondError(w http.ResponseWriter, r *http.Request) bool {
	http.Error(w, e.Error(), http.StatusTooManyRequests)
	return true
}
