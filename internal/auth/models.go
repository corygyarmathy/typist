// Package auth holds the domain types, business logic, and persistence
// for the auth bounded context.
//
// Layering:
//
//	handler.go      - HTTP transport (request parsing, response shaping)
//	service.go      - business logic, orchestrates repositories
//	repository.go   - data access, returns domain types not raw rows
//	models.go       - domain types shared across the layers
package auth

import (
	"errors"
)

type Token struct {
	Value     string // the signed JWT
	ExpiresIn int    // seconds until it expires
}

var ErrPasswordTooShort = errors.New("password length is too short") // -> 400
var ErrInvalidEmail = errors.New("provided email is invalid")        // -> 400
var ErrEmailTaken = errors.New("provided email is already taken")    // -> 409
var ErrReqBodyEmpty = errors.New("request body is empty")            // -> 400
