// Package uuid provides a simple wrapper around the github.com/google/uuid package to generate UUIDs.
package uuid

import "github.com/google/uuid"

func NewString() string {
	return uuid.Must(uuid.NewV7()).String()
}

func Validate(val string) bool {
	id, err := uuid.Parse(val)
	if err != nil {
		return false
	}
	return id.Version() == 7
}
