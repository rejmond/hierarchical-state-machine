package hsm

import (
	"errors"
)

var ErrorUnprocessableEntity = errors.New("unprocessable entity")
var ErrorInvalidState = errors.New("entity state is not valid")
var ErrorInvalidEntityType = errors.New("invalid entity type")
var ErrorProcessorNotFound = errors.New("processor not found")
