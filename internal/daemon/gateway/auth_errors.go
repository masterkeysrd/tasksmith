package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	loomauth "github.com/masterkeysrd/loom/auth"
	"github.com/openai/openai-go/v3"
	"golang.org/x/oauth2"
	"google.golang.org/genai"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
)

// AuthErrorKind classifies structured authentication failures reported to
// clients on [api.ErrorEvent.Code].
type AuthErrorKind string

const (
	AuthErrorExpired AuthErrorKind = AuthErrorKind(api.ErrCodeProviderAuthExpired)
	AuthErrorInvalid AuthErrorKind = AuthErrorKind(api.ErrCodeProviderAuthInvalid)
)

// ProviderAuthError wraps an authentication failure with the configured
// TaskSmith provider identity and an actionable user-facing message.
type ProviderAuthError struct {
	Provider     string
	ProviderType string
	Kind         AuthErrorKind
	OAuth        bool
	Cause        error
}

// Error returns the user-facing authentication message.
func (e *ProviderAuthError) Error() string {
	if e == nil {
		return ""
	}
	return e.UserMessage()
}

// Unwrap returns the underlying cause so sentinel checks such as
// [ErrMissingCredential] and [loomauth.ErrAuthRequired] continue to match.
func (e *ProviderAuthError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// UserMessage returns a concise, actionable message naming the configured
// TaskSmith provider rather than internal SDK wrapper noise.
func (e *ProviderAuthError) UserMessage() string {
	if e == nil {
		return ""
	}
	if e.OAuth {
		if errors.Is(e.Cause, ErrMissingCredential) {
			return fmt.Sprintf("Provider %q requires sign-in. Run /login %s (or `tasksmith auth login %s`) to authenticate.", e.Provider, e.Provider, e.Provider)
		}
		return fmt.Sprintf("Authentication for provider %q has expired. Run /login %s (or `tasksmith auth login %s`) to sign in again.", e.Provider, e.Provider, e.Provider)
	}
	if errors.Is(e.Cause, ErrMissingCredential) {
		if e.Cause != nil {
			if msg := strings.TrimSpace(e.Cause.Error()); msg != "" && strings.Contains(msg, e.Provider) {
				return msg + ". Update its credential or run /providers."
			}
		}
		return fmt.Sprintf("Provider %q is missing its API credential. Check its API key or run /providers.", e.Provider)
	}
	return fmt.Sprintf("Authentication failed for provider %q (401 Unauthorized). Check its API key or run /providers.", e.Provider)
}

var (
	oauthAuthMarkers = []string{
		"invalid_grant",
		"authentication token is expired",
		"provided authentication token is expired",
		"not logged in",
		"please run /login",
	}
	genericAuthMarkers = []string{
		"401 unauthorized",
		"status 401",
		"status code 401",
		"(401",
		"401:",
		"invalid api key",
		"invalid x-api-key",
		"authentication_error",
	}
)

// ClassifyAuthError inspects err and returns a [*ProviderAuthError] when it
// represents an authentication rejection or missing/expired credential.
// Context cancellation and deadline errors are never classified as auth errors.
func ClassifyAuthError(err error) (*ProviderAuthError, bool) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, false
	}
	var pae *ProviderAuthError
	if errors.As(err, &pae) && pae != nil {
		return pae, true
	}
	oauth, ok := classifyRawAuthError(err)
	if !ok {
		return nil, false
	}
	kind := AuthErrorInvalid
	if oauth {
		kind = AuthErrorExpired
	}
	return &ProviderAuthError{
		Kind:  kind,
		OAuth: oauth,
		Cause: err,
	}, true
}

func classifyRawAuthError(err error) (oauth bool, ok bool) {
	var openaiErr *openai.Error
	if errors.As(err, &openaiErr) && openaiErr != nil && openaiErr.StatusCode == http.StatusUnauthorized {
		return false, true
	}

	var anthropicErr *anthropic.Error
	if errors.As(err, &anthropicErr) && anthropicErr != nil && anthropicErr.StatusCode == http.StatusUnauthorized {
		return false, true
	}

	var genaiErr genai.APIError
	if errors.As(err, &genaiErr) && genaiErr.Code == http.StatusUnauthorized {
		return false, true
	}
	var genaiErrPtr *genai.APIError
	if errors.As(err, &genaiErrPtr) && genaiErrPtr != nil && genaiErrPtr.Code == http.StatusUnauthorized {
		return false, true
	}

	var retrieveErr *oauth2.RetrieveError
	if errors.As(err, &retrieveErr) && retrieveErr != nil {
		if strings.EqualFold(retrieveErr.ErrorCode, "invalid_grant") ||
			(retrieveErr.Response != nil && retrieveErr.Response.StatusCode == http.StatusUnauthorized) {
			return true, true
		}
	}

	if errors.Is(err, loomauth.ErrAuthRequired) {
		return true, true
	}
	if errors.Is(err, auth.ErrMissingCredential) {
		return false, true
	}

	msg := strings.ToLower(err.Error())
	for _, marker := range oauthAuthMarkers {
		if strings.Contains(msg, marker) {
			return true, true
		}
	}
	for _, marker := range genericAuthMarkers {
		if strings.Contains(msg, marker) {
			return false, true
		}
	}
	return false, false
}

func wrapProviderAuthError(inst *providerInstance, observer AuthObserver, err error) error {
	if err == nil || inst == nil {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrInvalidConfig) {
		return err
	}
	if _, isAuth := ClassifyAuthError(err); !isAuth {
		return err
	}

	oauth := inst.isOAuth()
	kind := AuthErrorInvalid
	state := auth.HealthInvalid
	if oauth {
		kind = AuthErrorExpired
		state = auth.HealthExpired
	}

	var existing *ProviderAuthError
	if errors.As(err, &existing) && existing != nil && existing.Provider == inst.cfg.Name {
		return err
	}

	if !errors.Is(err, ErrMissingCredential) && observer != nil {
		observer.RecordAuthFailure(inst.cfg.Name, inst.cfg.Type, state, inst.authRevision(), err.Error())
	}

	return &ProviderAuthError{
		Provider:     inst.cfg.Name,
		ProviderType: inst.cfg.Type,
		Kind:         kind,
		OAuth:        oauth,
		Cause:        err,
	}
}
