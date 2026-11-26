package harness

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/strutils"
	"github.com/masterkeysrd/tasksmith/internal/daemon/media"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

const (
	MetaSystemKind         = "tasksmith.system.kind"
	MetaCompactionTrigger  = "tasksmith.compaction.trigger"
	MetaCompactionFocus    = "tasksmith.compaction.focus"
	MetaCompactionRequests = "tasksmith.compaction.requests"
	// MetaRuleNames carries the list of rule names surfaced in a system reminder
	// (either auto-loaded or suggested) so consumers can correlate reminders with
	// the rules they reference.
	MetaRuleNames   = "tasksmith.rule.names"
	MetaAttachments = "tasksmith.attachments"
	// MetaPermissionReview carries the *api.PermissionReview receipt attached
	// to a tool result message when the auto-mode permission reviewer
	// approved the card instead of interrupting the human.
	MetaPermissionReview = "tasksmith.permission_review"
	// MetaUserIntent records the originating human prompt on a user message
	// whose body was expanded from a slash command or synthesized for an
	// autonomous subagent, so permission reviews evaluate against what the
	// human actually asked for.
	MetaUserIntent = "tasksmith.user_intent"
)

// SetUserIntent stamps the originating human intent string on msg. Passing an
// empty or whitespace-only string clears any stored override.
func SetUserIntent(msg *message.User, intent string) {
	if msg == nil {
		return
	}
	intent = strings.TrimSpace(intent)
	meta := msg.GetMetadata()
	if intent == "" {
		delete(meta, MetaUserIntent)
		return
	}
	if meta == nil {
		meta = make(map[string]any)
	}
	meta[MetaUserIntent] = intent
	msg.SetMetadata(meta)
}

// UserIntentOf returns the MetaUserIntent string recorded on msg, or "".
func UserIntentOf(msg message.Message) string {
	if msg == nil {
		return ""
	}
	val, _ := msg.GetMetadata()[MetaUserIntent].(string)
	return strings.TrimSpace(val)
}

// SetPermissionReview attaches the reviewer receipt to a tool result message.
// Any pre-existing value (e.g. a forged key echoed through structured tool
// content) is replaced, so a tool can never fabricate its own approval.
func SetPermissionReview(msg *message.Tool, review *api.PermissionReview) {
	if msg == nil {
		return
	}
	meta := msg.GetMetadata()
	if review == nil {
		delete(meta, MetaPermissionReview)
		return
	}
	if meta == nil {
		meta = make(map[string]any)
	}
	meta[MetaPermissionReview] = review
	msg.SetMetadata(meta)
}

// PermissionReviewOf retrieves the reviewer receipt from a message, handling
// both the in-memory *api.PermissionReview and the JSON-deserialized
// map[string]any form restored from the SQLite journal.
func PermissionReviewOf(msg message.Message) *api.PermissionReview {
	if msg == nil {
		return nil
	}
	raw, ok := msg.GetMetadata()[MetaPermissionReview]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case *api.PermissionReview:
		return v
	case api.PermissionReview:
		return &v
	case map[string]any:
		b, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var out api.PermissionReview
		if err := json.Unmarshal(b, &out); err != nil {
			return nil
		}
		return &out
	default:
		return nil
	}
}

// SetAttachments attaches structured api.Attachment references to a user message.
func SetAttachments(msg *message.User, atts []api.Attachment) {
	if msg == nil {
		return
	}
	meta := msg.GetMetadata()
	if len(atts) == 0 {
		delete(meta, MetaAttachments)
		return
	}
	if meta == nil {
		meta = make(map[string]any)
	}
	meta[MetaAttachments] = slices.Clone(atts)
	msg.SetMetadata(meta)
}

// AttachmentsOf retrieves structured api.Attachment references from a message,
// handling both in-memory []api.Attachment and JSON-deserialized []any (from SQLite journal/timeline).
func AttachmentsOf(msg message.Message) []api.Attachment {
	if msg == nil {
		return nil
	}
	raw, ok := msg.GetMetadata()[MetaAttachments]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []api.Attachment:
		return slices.Clone(v)
	case []any:
		if len(v) == 0 {
			return nil
		}
		b, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var out []api.Attachment
		if err := json.Unmarshal(b, &out); err != nil {
			return nil
		}
		return out
	default:
		return nil
	}
}

const (
	SystemKindCommand    = "command"
	SystemKindTask       = "task"
	SystemKindCompaction = "compaction"
	SystemKindSubagent   = "subagent"
	// SystemKindReminder marks a synthetic user message that carries an active or
	// suggested workspace rule inside <system_reminders>.
	SystemKindReminder = "reminder"
)

// SystemKindOf returns the "tasksmith.system.kind" metadata value on msg, or "" if unset.
func SystemKindOf(msg message.Message) string {
	if msg == nil {
		return ""
	}
	meta := msg.GetMetadata()
	if meta == nil {
		return ""
	}
	v, _ := meta[MetaSystemKind].(string)
	return v
}

// State holds the checkpointed conversation and permission state for a thread.
// It implements [github.com/masterkeysrd/loom/graph.State].
type State struct {
	Messages            message.MessageList               `json:"messages"`
	PendingCompaction   *api.CompactRequest               `json:"pending_compaction,omitempty"`
	PendingPermissions  []api.PermissionRequest           `json:"pending_permissions,omitempty"`
	PendingToolPlans    map[string]tools.Plan             `json:"pending_tool_plans,omitempty"`
	PermissionDecisions map[string]api.PermissionDecision `json:"permission_decisions,omitempty"`
	PendingQuestions    []api.QuestionRequest             `json:"pending_questions,omitempty"`
	QuestionResponses   map[string]api.QuestionResponse   `json:"question_responses,omitempty"`
	PendingPlanReviews  []api.PlanReviewRequest           `json:"pending_plan_reviews,omitempty"`
	PlanReviewDecisions map[string]api.PlanReviewDecision `json:"plan_review_decisions,omitempty"`
	Todos               []api.TodoItem                    `json:"todos,omitempty"`
	LoadedTools         []string                          `json:"loaded_tools,omitempty"`
	LoadedRules         []string                          `json:"loaded_rules,omitempty"`
	LoadedSkills        []string                          `json:"loaded_skills,omitempty"`
	SuggestedRules      []string                          `json:"suggested_rules,omitempty"`
	Mode                api.PermissionMode                `json:"mode,omitempty"`
	PlanPath            string                            `json:"plan_path,omitempty"`
	Context             api.ContextState                  `json:"context"`
}

// Copy returns a deep copy of the State so graph transitions do not mutate
// previously captured snapshots.
func (s State) Copy() State {
	msgs := make(message.MessageList, len(s.Messages))
	for i, m := range s.Messages {
		msgs[i] = message.CloneMessage(m)
	}

	var pending []api.PermissionRequest
	if len(s.PendingPermissions) > 0 {
		pending = clonePermissionRequests(s.PendingPermissions)
	}

	pendingToolPlans := maps.Clone(s.PendingToolPlans)
	for id, plan := range pendingToolPlans {
		plan.Paths = slices.Clone(plan.Paths)
		pendingToolPlans[id] = plan
	}

	var decisions map[string]api.PermissionDecision
	if len(s.PermissionDecisions) > 0 {
		decisions = maps.Clone(s.PermissionDecisions)
	}

	questions := cloneQuestionRequests(s.PendingQuestions)
	var responses map[string]api.QuestionResponse
	if s.QuestionResponses != nil {
		responses = make(map[string]api.QuestionResponse, len(s.QuestionResponses))
		for key, response := range s.QuestionResponses {
			responses[key] = *response.Clone()
		}
	}

	planReviews := clonePlanReviewRequests(s.PendingPlanReviews)
	var planDecisions map[string]api.PlanReviewDecision
	if s.PlanReviewDecisions != nil {
		planDecisions = make(map[string]api.PlanReviewDecision, len(s.PlanReviewDecisions))
		for key, dec := range s.PlanReviewDecisions {
			planDecisions[key] = *dec.Clone()
		}
	}

	var todos []api.TodoItem
	if len(s.Todos) > 0 {
		todos = slices.Clone(s.Todos)
	}

	var loadedTools []string
	if len(s.LoadedTools) > 0 {
		loadedTools = slices.Clone(s.LoadedTools)
	}

	var loadedRules []string
	if len(s.LoadedRules) > 0 {
		loadedRules = slices.Clone(s.LoadedRules)
	}

	var loadedSkills []string
	if len(s.LoadedSkills) > 0 {
		loadedSkills = slices.Clone(s.LoadedSkills)
	}

	var suggestedRules []string
	if len(s.SuggestedRules) > 0 {
		suggestedRules = slices.Clone(s.SuggestedRules)
	}

	var pendingCompact *api.CompactRequest
	if s.PendingCompaction != nil {
		pendingCompact = &api.CompactRequest{Focus: s.PendingCompaction.Focus}
	}

	return State{
		Messages:            msgs,
		PendingCompaction:   pendingCompact,
		PendingPermissions:  pending,
		PendingToolPlans:    pendingToolPlans,
		PermissionDecisions: decisions,
		PendingQuestions:    questions,
		QuestionResponses:   responses,
		PendingPlanReviews:  planReviews,
		PlanReviewDecisions: planDecisions,
		Todos:               todos,
		LoadedTools:         loadedTools,
		LoadedRules:         loadedRules,
		LoadedSkills:        loadedSkills,
		SuggestedRules:      suggestedRules,
		Mode:                s.Mode,
		PlanPath:            s.PlanPath,
		Context:             s.Context,
	}
}

// IsAutoCompacted reports whether the leading message was produced by auto-compaction.
func (s State) IsAutoCompacted() bool {
	if len(s.Messages) > 0 && SystemKindOf(s.Messages[0]) == SystemKindCompaction {
		if meta := s.Messages[0].GetMetadata(); meta != nil {
			if trig, ok := meta[MetaCompactionTrigger].(string); ok && trig == "auto" {
				return true
			}
		}
	}
	return false
}

// ShouldAutoCompact reports whether the current conversation state qualifies for
// mid-turn or entry auto-compaction.
func (s State) ShouldAutoCompact() bool {
	if s.HasPendingToolCalls() {
		return false
	}
	if s.Context.TriggerTokens <= 0 || s.Context.UsedTokens < s.Context.TriggerTokens {
		return false
	}
	if len(s.Messages) < 2 {
		return false
	}
	lastMsg := s.Messages[len(s.Messages)-1]
	return SystemKindOf(lastMsg) != SystemKindCompaction
}

// HasLeadingCompact reports whether there is a manual compaction request pending.
func (s State) HasLeadingCompact() bool {
	return s.PendingCompaction != nil
}

// ShouldWakeAgent checks if the latest message warrants waking the agent node.
func (s State) ShouldWakeAgent() bool {
	if len(s.Messages) == 0 {
		return false
	}
	last := s.Messages[len(s.Messages)-1]
	if _, isAssistant := last.(*message.Assistant); isAssistant {
		return false
	}
	return SystemKindOf(last) != SystemKindCommand
}

// LastUserMessageID returns the ID of the latest genuine [*message.User] message, or "" if none.
func (s State) LastUserMessageID() string {
	for _, m := range slices.Backward(s.Messages) {
		if u, ok := m.(*message.User); ok && u != nil && SystemKindOf(u) != SystemKindReminder && u.GetID() != "" {
			return u.GetID()
		}
	}
	return ""
}

const (
	latestUserIntentMaxRunes     = 600
	shortUserIntentRuneThreshold = 200
	priorShortUserIntentMaxRunes = 250
	priorLongUserIntentMaxRunes  = 200
	maxPriorShortUserIntentTurns = 2
	maxPriorLongUserIntentTurns  = 1
)

// LastUserIntentText returns a compact, token-budgeted projection of the most
// recent genuine user prompt (plus up to 1–2 cropped prior genuine user turns
// for follow-up context). Synthetic system messages (reminders, !cmd outputs,
// tasks, compactions, subagent traffic), assistant messages, tool results, and
// injected <system_reminders> blocks are excluded.
func (s State) LastUserIntentText() string {
	var turns []string
	var latestHasOverride bool

	for _, m := range slices.Backward(s.Messages) {
		u, ok := m.(*message.User)
		if !ok || u == nil || SystemKindOf(u) != "" {
			continue
		}

		text, hasOverride := extractUserIntentMessage(u)
		if text == "" {
			continue
		}

		if len(turns) == 0 {
			latestHasOverride = hasOverride
			turns = append(turns, text)
			if hasOverride {
				break
			}
			continue
		}

		maxPrior := maxPriorLongUserIntentTurns
		if utf8.RuneCountInString(turns[0]) < shortUserIntentRuneThreshold {
			maxPrior = maxPriorShortUserIntentTurns
		}
		if len(turns)-1 >= maxPrior {
			break
		}
		turns = append(turns, text)
	}

	if len(turns) == 0 {
		return ""
	}

	if latestHasOverride {
		return strings.TrimSpace(turns[0])
	}

	latest := cropIntentRunes(turns[0], latestUserIntentMaxRunes)
	if len(turns) == 1 {
		return latest
	}

	priorMax := priorLongUserIntentMaxRunes
	if utf8.RuneCountInString(turns[0]) < shortUserIntentRuneThreshold {
		priorMax = priorShortUserIntentMaxRunes
	}

	lines := make([]string, 0, len(turns))
	for i := len(turns) - 1; i >= 1; i-- {
		lines = append(lines, "[prev] "+cropIntentRunes(turns[i], priorMax))
	}
	lines = append(lines, "[latest] "+latest)
	return strings.Join(lines, "\n")
}

func extractUserIntentMessage(u *message.User) (string, bool) {
	if intent := UserIntentOf(u); intent != "" {
		return intent, true
	}

	var parts []string
	for _, b := range u.Content {
		tb, ok := b.(*message.TextBlock)
		if !ok {
			continue
		}
		text := strings.TrimSpace(tb.Text)
		if text == "" || strings.HasPrefix(text, "<system_reminders>") {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n"), false
}

func cropIntentRunes(s string, maxBytes int) string {
	s = strings.TrimSpace(s)
	out, _ := strutils.TruncateLine(s, maxBytes, "…")
	return out
}

type MessageUpdateOptions struct {
	Static                   StaticContext
	Limits                   models.Limits
	SkipContextRecalculation bool
}

func (s State) ApplyCompaction(static StaticContext, limits models.Limits) State {
	s.PendingCompaction = nil
	// The messages these counts describe were just replaced by the summary, so
	// drop them: computeContextState treats a higher tracked count as evidence it
	// is undercounting, and after compaction that prior value is no longer true.
	s.Context.UsedTokens = 0
	s.Context.CachedTokens = 0
	s.Context.Compactions++
	s.Context.LastCompacted = time.Now().UTC()
	s.LoadedTools = nil
	s.LoadedRules = nil
	s.LoadedSkills = nil
	s.SuggestedRules = nil
	s.Mode = ""
	s.Context = computeContextState(static, s, limits)
	return s
}

func (s State) PrependMessage(msg message.Message, opts MessageUpdateOptions) State {
	s.Messages = append(message.MessageList{msg}, s.Messages...)
	if !opts.SkipContextRecalculation {
		s.Context = computeContextState(opts.Static, s, opts.Limits)
	}
	return s
}

func (s State) AppendMessage(msg message.Message, opts MessageUpdateOptions) State {
	return s.AppendMessages(message.MessageList{msg}, opts)
}

func (s State) AppendMessages(msgs message.MessageList, opts MessageUpdateOptions) State {
	s.Messages = append(s.Messages, msgs...)
	if !opts.SkipContextRecalculation {
		s.Context = computeContextState(opts.Static, s, opts.Limits)
	}
	return s
}

// toolBlockName returns the tool name referenced by a tool reference/definition
// block, or "" for any other block kind.
func toolBlockName(b message.Block) string {
	switch blk := b.(type) {
	case *message.ToolReferenceBlock:
		return blk.ToolName
	case *message.ToolDefinitionBlock:
		return blk.Name
	default:
		return ""
	}
}

// DeduplicateToolBlocks filters *message.ToolReferenceBlock and
// *message.ToolDefinitionBlock entries out of msg when they reference a tool that
// is already present in s.LoadedTools (or already appears earlier in the same
// message), returning the (possibly filtered) message and the names of the tools
// newly activated by this message. Tool names are compared case-insensitively.
//
// If msg contains no tool reference/definition blocks, it is returned unchanged
// with a nil slice. For a *message.Tool whose content becomes empty after
// filtering (i.e. every tool reference was already loaded), a text block
// informing the model that the tools are already active is appended so the tool
// result message remains non-empty.
func (s State) DeduplicateToolBlocks(msg message.Message) (message.Message, []string) {
	if msg == nil {
		return msg, nil
	}

	var content message.Content
	switch m := msg.(type) {
	case *message.User:
		content = m.Content
	case *message.Tool:
		content = m.Content
	default:
		return msg, nil
	}

	// Fast path: no tool reference/definition blocks to deduplicate.
	hasToolBlock := false
	for _, b := range content {
		if toolBlockName(b) != "" {
			hasToolBlock = true
			break
		}
	}
	if !hasToolBlock {
		return msg, nil
	}

	loaded := make(map[string]struct{}, len(s.LoadedTools))
	for _, name := range s.LoadedTools {
		loaded[strings.ToLower(name)] = struct{}{}
	}

	var newlyLoaded, alreadyLoaded []string
	filtered := make(message.Content, 0, len(content))
	for _, b := range content {
		name := toolBlockName(b)
		if name == "" {
			filtered = append(filtered, b)
			continue
		}
		key := strings.ToLower(name)
		if _, ok := loaded[key]; ok {
			alreadyLoaded = append(alreadyLoaded, name)
			continue
		}
		loaded[key] = struct{}{}
		newlyLoaded = append(newlyLoaded, name)
		filtered = append(filtered, b)
	}

	cloned := message.CloneMessage(msg)
	switch m := cloned.(type) {
	case *message.User:
		m.Content = filtered
	case *message.Tool:
		m.Content = filtered
		if len(m.Content) == 0 {
			m.Content = append(m.Content, &message.TextBlock{Text: fmt.Sprintf("Tools already loaded in context: %s", strings.Join(alreadyLoaded, ", "))})
		}
	}
	return cloned, newlyLoaded
}

// RecordLoadedTools returns a copy of s with the given tool names appended to
// LoadedTools, deduplicated case-insensitively against the existing entries.
func (s State) RecordLoadedTools(names ...string) State {
	if len(names) == 0 {
		return s
	}
	seen := make(map[string]struct{}, len(s.LoadedTools)+len(names))
	for _, name := range s.LoadedTools {
		seen[strings.ToLower(name)] = struct{}{}
	}
	for _, name := range names {
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		s.LoadedTools = append(s.LoadedTools, name)
	}
	return s
}

// HasLoadedRule reports whether a rule with the given name is already in
// LoadedRules (case-insensitive).
func (s State) HasLoadedRule(name string) bool {
	key := strings.ToLower(name)
	for _, existing := range s.LoadedRules {
		if strings.ToLower(existing) == key {
			return true
		}
	}
	return false
}

// RecordLoadedRules returns a copy of s with the given rule names appended to
// LoadedRules, deduplicated case-insensitively against the existing entries.
func (s State) RecordLoadedRules(names ...string) State {
	if len(names) == 0 {
		return s
	}
	seen := make(map[string]struct{}, len(s.LoadedRules)+len(names))
	for _, name := range s.LoadedRules {
		seen[strings.ToLower(name)] = struct{}{}
	}
	for _, name := range names {
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		s.LoadedRules = append(s.LoadedRules, name)
	}
	return s
}

// HasLoadedSkill reports whether a skill with the given name is already in
// LoadedSkills (case-insensitive).
func (s State) HasLoadedSkill(name string) bool {
	key := strings.ToLower(name)
	for _, existing := range s.LoadedSkills {
		if strings.ToLower(existing) == key {
			return true
		}
	}
	return false
}

// RecordLoadedSkills returns a copy of s with the given skill names appended to
// LoadedSkills, deduplicated case-insensitively against the existing entries.
func (s State) RecordLoadedSkills(names ...string) State {
	if len(names) == 0 {
		return s
	}
	seen := make(map[string]struct{}, len(s.LoadedSkills)+len(names))
	for _, name := range s.LoadedSkills {
		seen[strings.ToLower(name)] = struct{}{}
	}
	for _, name := range names {
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		s.LoadedSkills = append(s.LoadedSkills, name)
	}
	return s
}

// HasSuggestedRule reports whether a rule with the given name is already in
// SuggestedRules (case-insensitive).
func (s State) HasSuggestedRule(name string) bool {
	key := strings.ToLower(name)
	for _, existing := range s.SuggestedRules {
		if strings.ToLower(existing) == key {
			return true
		}
	}
	return false
}

// RecordSuggestedRules returns a copy of s with the given rule names appended to
// SuggestedRules, deduplicated case-insensitively against the existing entries.
func (s State) RecordSuggestedRules(names ...string) State {
	if len(names) == 0 {
		return s
	}
	seen := make(map[string]struct{}, len(s.SuggestedRules)+len(names))
	for _, name := range s.SuggestedRules {
		seen[strings.ToLower(name)] = struct{}{}
	}
	for _, name := range names {
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		s.SuggestedRules = append(s.SuggestedRules, name)
	}
	return s
}

// HasPendingToolCalls reports whether the latest assistant message in the state
// has one or more unfulfilled tool calls.
func (s State) HasPendingToolCalls() bool {
	_, _, unfulfilled, _ := s.ActiveToolCalls()
	return len(unfulfilled) > 0
}

// ActiveModalities returns the sorted, deduplicated set of non-text input
// modalities ("image", "pdf", "audio") currently present in State.Messages
// (post-compaction live context).
func (s State) ActiveModalities() []media.Modality {
	seen := make(map[media.Modality]struct{}, 3)
	for _, msg := range s.Messages {
		if msg == nil {
			continue
		}
		for _, blk := range msg.GetContent() {
			if mod, ok := media.ModalityOfBlock(blk); ok {
				seen[mod] = struct{}{}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]media.Modality, 0, len(seen))
	for mod := range seen {
		out = append(out, mod)
	}
	slices.Sort(out)
	return out
}

// ActiveToolCalls locates the latest [*message.Assistant] message with tool calls
// and returns its index, the assistant message, any unfulfilled tool calls, and
// whether a new user message was appended after that assistant turn.
func (s State) ActiveToolCalls() (int, *message.Assistant, []*message.ToolCall, bool) {
	for i, v := range slices.Backward(s.Messages) {
		asst, ok := v.(*message.Assistant)
		if !ok || asst == nil || len(asst.ToolCalls()) == 0 {
			continue
		}

		fulfilled := make(map[string]bool)
		hasUserAfter := false
		for j := i + 1; j < len(s.Messages); j++ {
			switch m := s.Messages[j].(type) {
			case *message.Tool:
				fulfilled[m.ToolCallID] = true
			case *message.User:
				hasUserAfter = true
			}
		}

		var unfulfilled []*message.ToolCall
		for _, tc := range asst.ToolCalls() {
			if tc != nil && !fulfilled[tc.ID] {
				unfulfilled = append(unfulfilled, tc)
			}
		}

		if len(unfulfilled) > 0 {
			return i, asst, unfulfilled, hasUserAfter
		}
		return -1, nil, nil, false
	}
	return -1, nil, nil, false
}

func clonePermissionRequests(in []api.PermissionRequest) []api.PermissionRequest {
	if len(in) == 0 {
		return nil
	}
	out := make([]api.PermissionRequest, len(in))
	for i, r := range in {
		r.GrantPatterns = slices.Clone(r.GrantPatterns)
		out[i] = r
	}
	return out
}

func cloneQuestionRequests(in []api.QuestionRequest) []api.QuestionRequest {
	if len(in) == 0 {
		return nil
	}
	out := make([]api.QuestionRequest, len(in))
	for i := range in {
		out[i] = *in[i].Clone()
	}
	return out
}

func clonePlanReviewRequests(in []api.PlanReviewRequest) []api.PlanReviewRequest {
	if len(in) == 0 {
		return nil
	}
	out := make([]api.PlanReviewRequest, len(in))
	for i := range in {
		out[i] = *in[i].Clone()
	}
	return out
}
