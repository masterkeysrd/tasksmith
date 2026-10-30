package server

import (
	"context"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
)

type SystemBackend interface {
	Health(ctx context.Context) (*api.HealthResponse, error)
	Shutdown(ctx context.Context) error
}

type EventBackend interface {
	AddSubscriber(ctx context.Context, subscriberID, since string, topics ...string) (*broker.Subscriber, []string, error)
	RemoveSubscriber(sub *broker.Subscriber)
	Subscribe(ctx context.Context, subscriberID, since string, topics ...string) ([]string, error)
	Unsubscribe(ctx context.Context, subscriberID string, topics ...string) ([]string, error)
	FormatEvent(msg broker.Message) (api.Event, error)
	Done() <-chan struct{}
}

type WorkspaceBackend interface {
	GetWorkspace(ctx context.Context, workspace string) (*api.Workspace, error)
	ReloadWorkspace(ctx context.Context, workspace string) error
	ListAgents(ctx context.Context, workspace string) ([]api.Agent, error)
	GetAgent(ctx context.Context, workspace, name string) (*api.Agent, error)
	ListCommands(ctx context.Context, workspace string) ([]api.Command, error)
	GetCommand(ctx context.Context, workspace, name string) (*api.Command, error)
	ListSkills(ctx context.Context, workspace string) ([]api.Skill, error)
	GetSkill(ctx context.Context, workspace, name string) (*api.Skill, error)
	ListModels(ctx context.Context, workspace string) ([]api.Model, error)
	GetModel(ctx context.Context, workspace, id string) (*api.Model, error)
	GetPermissions(ctx context.Context, workspace string) (*api.PermissionsResponse, error)
	AddPermissionRule(ctx context.Context, workspace string, req api.PermissionRuleRequest) (*api.PermissionsResponse, error)
	RemovePermissionRule(ctx context.Context, workspace string, req api.PermissionRuleRequest) (*api.PermissionsResponse, error)
}

type SessionBackend interface {
	CreateSession(ctx context.Context, workspace string, req api.CreateSessionRequest) (*api.Session, bool, error)
	PreviewSession(ctx context.Context, workspace string, req api.PreviewSessionRequest) (*api.SessionPreview, error)
	ListSessions(ctx context.Context, workspace string) ([]api.Session, error)
	GetSession(ctx context.Context, workspace, id string) (*api.Session, error)
	UpdateSession(ctx context.Context, workspace, id string, req api.UpdateSessionRequest) (*api.Session, error)
	DeleteSession(ctx context.Context, workspace, id string) error
	Timeline(ctx context.Context, workspace, id string) ([]api.TimelineEntry, error)
	Send(ctx context.Context, workspace, id string, req api.SendRequest) error
	Cancel(ctx context.Context, workspace, id string) error

	Changes(ctx context.Context, workspace, id string) ([]api.FileChange, error)
	Diff(ctx context.Context, workspace, id, path, entryID string) (*api.DiffResponse, error)
	Revert(ctx context.Context, workspace, id string, req api.RevertRequest) error

	Tasks(ctx context.Context, workspace, id string) ([]api.Task, error)
	KillTask(ctx context.Context, workspace, id, taskID string) error
	StreamTaskOutput(ctx context.Context, workspace, id, taskID string) (<-chan string, string, func(), error)
}

type Backend interface {
	SystemBackend
	EventBackend
	WorkspaceBackend
	SessionBackend
}
