// Package uuid provides a simple wrapper around the github.com/google/uuid package to generate UUIDs.
package uuid

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func NewString() string {
	return uuid.Must(uuid.NewV7()).String()
}

// Monotonic builds a UUIDv7-shaped, lexicographically increasing ID from a
// shared base timestamp and sequence number. Google's uuid.NewV7 only
// guarantees millisecond resolution, so a burst of IDs generated within the
// same millisecond could interleave; the sequence occupies the random prefix
// instead, preserving generation order under lexicographic scans.
func Monotonic(base time.Time, seq int) string {
	var id uuid.UUID
	ms := base.UnixMilli() + int64(seq/4096)
	binary.BigEndian.PutUint64(id[:8], uint64(ms)<<16)
	id[6] = 0x70 | byte(seq>>8&0x0F)
	id[7] = byte(seq)
	id[8] = 0x80
	rnd := uuid.Must(uuid.NewRandom())
	copy(id[9:], rnd[9:])
	return id.String()
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
