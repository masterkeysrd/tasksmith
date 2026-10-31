// Package uuid provides a simple wrapper around the github.com/google/uuid package to generate UUIDs.
package uuid

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

func NewString() string {
	return uuid.Must(uuid.NewV7()).String()
}

func Validate(val string) bool {
	id, err := uuid.Parse(val)
	if err != nil {
		return false
	}
	return id.Version() == 7 && id.String() == val
}

func ExtractTime(val string) (time.Time, error) {
	id, err := uuid.Parse(val)
	if err != nil {
		return time.Time{}, err
	}
	if id.Version() != 7 {
		return time.Time{}, fmt.Errorf("invalid UUID version: %d", id.Version())
	}

	sec, nsec := id.Time().UnixTime()
	return time.Unix(sec, nsec).UTC(), nil
}
