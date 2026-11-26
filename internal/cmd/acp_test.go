package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
)

type acpEditor struct {
	sdk.Client
	updates chan sdk.SessionUpdate
}

func (e *acpEditor) SessionUpdate(_ context.Context, n sdk.SessionNotification) error {
	e.updates <- n.Update
	return nil
}

func TestACPCommandRoundTrip(t *testing.T) {
	_, sock, ws := setupCLITestDaemon(t, &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
			if isTitleGeneratorRequest(req) {
				return titleResponse(req), nil
			}
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: "provider-id"},
					Model:     req.Model,
					Content: []message.Block{
						&message.ThinkingBlock{Thinking: "thinking"},
						&message.TextBlock{Text: "reply"},
					},
					Done: true,
				}, nil)
			}, nil
		},
	})
	execWorkspace(t, ws)

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	in, send := io.Pipe()
	out, recv := io.Pipe()
	defer func() {
		_ = in.Close()
		_ = out.Close()
		_ = recv.Close()
		_ = send.Close()
	}()

	var frames bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetArgs([]string{
		"acp",
		"--socket",
		sock,
	})
	cmd.SetIn(in)
	cmd.SetOut(io.MultiWriter(&frames, recv))
	cmd.SetErr(io.Discard)

	done := make(chan error, 1)
	go func() {
		done <- cmd.ExecuteContext(ctx)
	}()

	editor := &acpEditor{updates: make(chan sdk.SessionUpdate, 128)}
	peer := sdk.NewClientSideConnection(editor, send, out)

	init, err := peer.Initialize(ctx, sdk.InitializeRequest{ProtocolVersion: sdk.ProtocolVersionNumber})
	if err != nil ||
		!init.AgentCapabilities.LoadSession ||
		!init.AgentCapabilities.PromptCapabilities.Image ||
		!init.AgentCapabilities.PromptCapabilities.EmbeddedContext {
		t.Fatalf("initialize: %+v %v", init, err)
	}

	created, err := peer.NewSession(ctx, sdk.NewSessionRequest{
		Cwd:        ws,
		McpServers: []sdk.McpServer{},
	})
	if err != nil || len(created.Modes.AvailableModes) != 5 {
		t.Fatalf("create: %+v %v", created, err)
	}
	id := created.SessionId

	if _, err := peer.SetSessionMode(ctx, sdk.SetSessionModeRequest{
		SessionId: id,
		ModeId:    "invalid",
	}); err == nil {
		t.Fatal("invalid mode accepted")
	} else if rpc, ok := errors.AsType[*sdk.RequestError](err); !ok || rpc.Code != -32602 {
		t.Fatalf("invalid mode: %v", err)
	}

	_, err = peer.SetSessionMode(ctx, sdk.SetSessionModeRequest{
		SessionId: id,
		ModeId:    "ask",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, text := range []string{
		"hello",
		"hello again",
	} {
		r, err := peer.Prompt(ctx, sdk.PromptRequest{
			SessionId: id,
			Prompt:    []sdk.ContentBlock{sdk.TextBlock(text)},
		})
		if err != nil || r.StopReason != sdk.StopReasonEndTurn {
			t.Fatalf("prompt: %+v %v", r, err)
		}
	}

	loaded, err := peer.LoadSession(ctx, sdk.LoadSessionRequest{
		SessionId:  id,
		Cwd:        ws,
		McpServers: []sdk.McpServer{},
	})
	if err != nil || loaded.Modes.CurrentModeId != "ask" {
		t.Fatalf("load: %+v %v", loaded, err)
	}

	if err = send.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	seen := map[string]int{}
	newResponse := false
	for line := range strings.SplitSeq(strings.TrimSpace(frames.String()), "\n") {
		var f struct {
			Result  struct{ SessionID sdk.SessionId }
			JSONRPC string
			Params  struct {
				Update struct {
					Content           struct{ Text string }
					SessionUpdate     string
					AvailableCommands []sdk.AvailableCommand
				}
			}
		}
		if err = json.Unmarshal([]byte(line), &f); err != nil || f.JSONRPC != "2.0" {
			t.Fatalf("invalid ACP stdout: %q", line)
		}

		if f.Result.SessionID == id {
			newResponse = true
		}
		if f.Params.Update.SessionUpdate == "available_commands_update" && !newResponse {
			t.Fatal("command notification preceded session/new response")
		}

		u := f.Params.Update
		if (u.SessionUpdate == "agent_message_chunk" && u.Content.Text != "reply") ||
			(u.SessionUpdate == "agent_thought_chunk" && u.Content.Text != "thinking") {
			t.Fatalf("unexpected content: %+v", u)
		}

		seen[f.Params.Update.SessionUpdate]++
		for _, c := range f.Params.Update.AvailableCommands {
			seen["/"+c.Name]++
		}
	}

	if seen["/compact"] != 2 ||
		seen["user_message_chunk"] != 2 ||
		seen["agent_message_chunk"] != 4 ||
		seen["agent_thought_chunk"] != 4 ||
		seen["current_mode_update"] != 1 {
		t.Fatalf("updates: %v", seen)
	}
}
