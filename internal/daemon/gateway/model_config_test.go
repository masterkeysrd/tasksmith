package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/warp"
)

func TestEffectiveTemperatureConfig(t *testing.T) {
	for _, tc := range []struct {
		name     string
		base     *profile.Profile
		override *bool
		wantTemp bool
		warning  string
	}{
		{"unknown", nil, nil, true, "temperature_unknown"},
		{"supported", &profile.Profile{Capabilities: profile.Capabilities{Temperature: true}}, nil, true, ""},
		{"unsupported", &profile.Profile{}, nil, false, "temperature_unsupported"},
		{"override true", &profile.Profile{}, new(true), true, ""},
		{"override false", &profile.Profile{Capabilities: profile.Capabilities{Temperature: true}}, new(false), false, "temperature_unsupported"},
		{"workspace only true", nil, new(true), true, ""},
		{"workspace only false", nil, new(false), false, "temperature_unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &thinkingTestProvider{modelProfile: tc.base}
			spec := &warp.ProviderModel{ID: "model", Limits: warp.ProviderModelLimits{Context: 4000, Output: 1000}, Capabilities: &warp.ProviderModelCapabilities{Completion: new(true), Temperature: tc.override}}
			effective := models.Merge("provider/model", "ollama", spec, tc.base)
			g := &gatewayImpl{providers: map[string]*providerInstance{"provider": {cfg: ProviderConfig{Name: "provider", Type: "ollama"}, llmProv: p}}}
			zero := float32(0)
			resolved, diagnostics, err := models.Configure(effective, models.Preferences{Temperature: &zero})
			if err != nil {
				t.Fatal(err)
			}
			if (resolved.Temperature != nil) != tc.wantTemp {
				t.Fatalf("temperature: %+v", resolved.Temperature)
			}
			if zero != 0 {
				t.Fatal("request mutated")
			}
			if resolved.MaxTokens != 1000 || resolved.ContextWindow != 4000 {
				t.Fatalf("config: %+v", resolved)
			}
			if tc.warning == "" && len(diagnostics) != 0 || tc.warning != "" && (len(diagnostics) != 1 || diagnostics[0].Code != tc.warning) {
				t.Fatalf("diagnostics: %+v", diagnostics)
			}
			model, err := g.LLM(context.Background(), effective, resolved)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := model.Stream(context.Background(), []message.Message{message.NewUserText("test")})
			if err != nil {
				t.Fatal(err)
			}
			for range stream {
			}
			if (p.request.Temperature != nil) != tc.wantTemp || p.request.MaxTokens != 1000 || p.request.ContextWindow != 4000 {
				t.Fatalf("request: %+v", p.request)
			}
		})
	}
}

func TestEffectiveProfileAndSettings(t *testing.T) {
	base := &profile.Profile{Limits: profile.Limits{Context: 100, Output: 10, Dimensions: 1536}, Capabilities: profile.Capabilities{Completion: true, Decision: true, Embedding: true, Attachment: true, ToolCall: true, Temperature: true, ReasoningOptions: []profile.ReasoningOption{{Type: "effort", Values: []string{"low"}}}}, Modalities: profile.Modalities{Inputs: []profile.Modality{"text"}, Outputs: []profile.Modality{"text"}}}
	caps := &warp.ProviderModelCapabilities{Completion: new(false), Decision: new(false), Embedding: new(false), Attachment: new(false), Tools: new(false), Temperature: new(false), StructuredOutput: new(true), Dimensions: []int{384, 768}, Thinking: []warp.ProviderModelThinkingCapability{{Type: "effort", AllowedEfforts: []string{"high"}}}, Modalities: &warp.ProviderModelModalities{Input: []string{"image"}}}
	spec := &warp.ProviderModel{ID: "model", Limits: warp.ProviderModelLimits{Context: 200, Output: 20, Dimensions: 384}, Capabilities: caps}
	model := models.Merge("local/model", "ollama", spec, base)
	p := model.Profile()
	if p.Capabilities.Completion || p.Capabilities.Decision || p.Capabilities.Embedding || p.Capabilities.Attachment || p.Capabilities.ToolCall || p.Capabilities.Temperature || p.Limits.Context != 200 || p.Limits.Output != 20 || p.Limits.Dimensions != 384 || len(p.Modalities.Inputs) != 1 || p.Modalities.Inputs[0] != "image" || len(p.Modalities.Outputs) != 1 || p.Modalities.Outputs[0] != "text" {
		t.Fatalf("effective profile: %+v", p)
	}
	value := float32(0)
	settings := models.Preferences{Temperature: &value, AgentThinking: &api.ThinkingSetting{Type: "effort", Effort: "low"}, SessionThinking: &api.ThinkingSetting{Type: "effort", Effort: "high"}}
	resolved, diagnostics, err := models.Configure(model, settings)
	if err != nil || model.Capabilities.StructuredOutput == nil || !*model.Capabilities.StructuredOutput || len(model.Capabilities.Dimensions) != 2 || resolved.Thinking == nil || resolved.Thinking.Effort != "high" || resolved.Temperature != nil {
		t.Fatalf("effective settings: %+v, %+v, %v", resolved, diagnostics, err)
	}
	settings.SessionThinking = nil
	resolved, diagnostics, err = models.Configure(model, settings)
	if err != nil || resolved.Thinking != nil || len(diagnostics) != 2 || diagnostics[0].Code != "thinking_unsupported" {
		t.Fatalf("unsupported agent defaults: %+v, %+v, %v", resolved, diagnostics, err)
	}
	settings.SessionThinking = &api.ThinkingSetting{Type: "effort", Effort: "low"}
	if _, _, err := models.Configure(model, settings); err == nil {
		t.Fatal("unsupported explicit thinking accepted")
	}
}

func TestRequestCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name      string
		base      *profile.Profile
		override  *bool
		wantError bool
	}{
		{"unknown", nil, nil, false},
		{"supported profile", &profile.Profile{Capabilities: profile.Capabilities{ToolCall: true}}, nil, false},
		{"unsupported profile", &profile.Profile{}, nil, true},
		{"workspace unsupported", nil, new(false), true},
		{"workspace supported", nil, new(true), false},
		{"override supported", &profile.Profile{}, new(true), false},
		{"override unsupported", &profile.Profile{Capabilities: profile.Capabilities{ToolCall: true}}, new(false), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &thinkingTestProvider{modelProfile: tc.base}
			spec := &warp.ProviderModel{ID: "model", Capabilities: &warp.ProviderModelCapabilities{Completion: new(true), Tools: tc.override}}
			effective := models.Merge("provider/model", "ollama", spec, tc.base)
			g := &gatewayImpl{providers: map[string]*providerInstance{"provider": {cfg: ProviderConfig{Name: "provider", Type: "ollama"}, llmProv: p}}}
			model, err := g.LLM(context.Background(), effective, llm.ModelConfig{})
			if err != nil {
				t.Fatal(err)
			}
			model = model.BindToolDefs(tool.Definition{Name: "search"})
			stream, err := model.Stream(context.Background(), []message.Message{message.NewUserText("test")})
			if tc.wantError {
				if !errors.Is(err, ErrUnsupportedCapability) || !strings.Contains(err.Error(), "tools") || p.request != nil {
					t.Fatalf("error=%v request=%+v", err, p.request)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for range stream {
			}
			if p.request == nil || len(p.request.Tools) != 1 || p.request.Tools[0].Name != "search" {
				t.Fatalf("request: %+v", p.request)
			}
		})
	}
}

func TestRequestContentValidation(t *testing.T) {
	text := message.NewUserText("text")
	image := message.NewUser(&message.ImageBlock{Data: []byte("image"), MIMEType: "image/png"})
	urlImage := message.NewUserImageURL("https://example.test/image")
	audio := message.NewUser(&message.AudioBlock{Data: []byte("audio"), MIMEType: "audio/wav"})
	history := []message.Message{&message.Assistant{Content: message.Content{&message.ToolCall{ID: "call", Name: "search"}}}, &message.Tool{ToolCallID: "call"}}
	for _, tc := range []struct {
		name string
		prof models.Model
		req  *llm.Request
		want string
	}{
		{"text without tools", models.Model{Capabilities: models.Capabilities{Tools: new(false)}}, &llm.Request{Messages: []message.Message{text}}, ""},
		{"text unsupported", models.Model{Modalities: models.Modalities{Input: []string{"image"}}}, &llm.Request{Messages: []message.Message{text}}, "text input"},
		{"tool history", models.Model{Capabilities: models.Capabilities{Tools: new(false)}}, &llm.Request{Messages: history}, "tool history"},
		{"unknown history", models.Model{}, &llm.Request{Messages: history}, ""},
		{"image supported", models.Model{ProviderType: "ollama", Modalities: models.Modalities{Input: []string{"image"}}}, &llm.Request{Messages: []message.Message{image}}, ""},
		{"image unsupported", models.Model{ProviderType: "ollama", Modalities: models.Modalities{Input: []string{"text"}}}, &llm.Request{Messages: []message.Message{text, image}}, "image input"},
		{"unknown modality", models.Model{ProviderType: "ollama"}, &llm.Request{Messages: []message.Message{image}}, ""},
		{"empty modalities", models.Model{ProviderType: "ollama", Modalities: models.Modalities{Input: []string{}}}, &llm.Request{Messages: []message.Message{image}}, "image input"},
		{"ollama image url", models.Model{ProviderType: "ollama"}, &llm.Request{Messages: []message.Message{urlImage}}, "cannot safely send"},
		{"openai audio", models.Model{ProviderType: "openai"}, &llm.Request{Messages: []message.Message{audio}}, "cannot safely send"},
		{"genai audio", models.Model{ProviderType: "genai"}, &llm.Request{Messages: []message.Message{audio}}, ""},
		{"assistant image", models.Model{ProviderType: "genai"}, &llm.Request{Messages: []message.Message{&message.Assistant{Content: image.Content}}}, "cannot safely send"},
		{"unknown block", models.Model{}, &llm.Request{Messages: []message.Message{message.NewUser(&message.ToolCallChunk{})}}, "cannot safely send"},
		{"user tool call", models.Model{}, &llm.Request{Messages: []message.Message{message.NewUser(&message.ToolCall{})}}, "cannot safely send"},
		{"unsupported schema", models.Model{Capabilities: models.Capabilities{StructuredOutput: new(false)}}, &llm.Request{ResponseSchema: &jsonschema.Schema{}}, "structured output"},
		{"anthropic schema supported", models.Model{ProviderType: "anthropic", Capabilities: models.Capabilities{StructuredOutput: new(true)}}, &llm.Request{ResponseSchema: &jsonschema.Schema{}}, ""},
		{"anthropic schema unknown", models.Model{ProviderType: "anthropic"}, &llm.Request{ResponseSchema: &jsonschema.Schema{}}, ""},
		{"anthropic json", models.Model{ProviderType: "anthropic"}, &llm.Request{ResponseFormat: "json_object"}, ""},
		{"openai schema and json", models.Model{ProviderType: "openai", Capabilities: models.Capabilities{StructuredOutput: new(true)}}, &llm.Request{ResponseSchema: &jsonschema.Schema{}, ResponseFormat: "json_object"}, ""},
		{"anthropic schema unsupported", models.Model{ProviderType: "anthropic", Capabilities: models.Capabilities{StructuredOutput: new(false)}}, &llm.Request{ResponseSchema: &jsonschema.Schema{}}, "structured output"},
		{"openai schema unsupported", models.Model{ProviderType: "openai", Capabilities: models.Capabilities{StructuredOutput: new(false)}}, &llm.Request{ResponseSchema: &jsonschema.Schema{}}, "structured output"},
		{"unknown schema", models.Model{}, &llm.Request{ResponseSchema: &jsonschema.Schema{}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.req.Model = "model"
			err := validateRequest(tc.prof, tc.req)
			if tc.want == "" && err != nil || tc.want != "" && (!errors.Is(err, ErrUnsupportedCapability) || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("error: %v", err)
			}
		})
	}
}

func TestAnthropicThinkingTemperatureConflict(t *testing.T) {
	v := float32(0.5)
	m := models.Model{ProviderType: "anthropic", Capabilities: models.Capabilities{Temperature: new(true), Thinking: []models.ThinkingOption{{Type: "budget_tokens", Min: 1, Max: 4096}}}}
	cfg, diagnostics, err := models.Configure(m, models.Preferences{Temperature: &v, SessionThinking: &api.ThinkingSetting{Type: "budget", Budget: 1024}})
	if err != nil || cfg.Temperature != nil || len(diagnostics) != 1 || diagnostics[0].Code != "temperature_thinking_conflict" {
		t.Fatalf("result: %+v, %+v, %v", cfg, diagnostics, err)
	}
}
