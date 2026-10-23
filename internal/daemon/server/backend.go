package server

import (
	"context"

	"github.com/masterkeysrd/tasksmith/api"
)

type Backend interface {
	ListSessions(ctx context.Context) ([]*api.Session, error)
}
