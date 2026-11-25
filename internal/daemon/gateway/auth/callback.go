package auth

import (
	_ "embed"
	"fmt"
	"html/template"
	"net/url"
	"strings"
)

//go:embed oauth_callback.html
var oauthCallbackHTML string

var oauthCallbackTemplate = template.Must(template.New("oauth_callback").Parse(oauthCallbackHTML))

// ParseCallbackInput extracts the authorization code and optional state from a
// callback URL pasted by hand. It accepts a full callback URL, a query string,
// a fragment parameter list, or a bare authorization code.
func ParseCallbackInput(raw string) (code, state string, err error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", "", fmt.Errorf("%w: callback url is required", ErrInvalidCallback)
	}
	if strings.Contains(value, "://") && !strings.Contains(value, "?") && !strings.Contains(value, "#") {
		return "", "", fmt.Errorf("%w: callback url carries no authorization code", ErrInvalidCallback)
	}

	query := value
	if idx := strings.IndexAny(value, "?#"); idx >= 0 {
		query = value[idx+1:]
	}
	if hash := strings.LastIndex(query, "#"); hash >= 0 {
		query = query[:hash]
	}

	params, parseErr := url.ParseQuery(query)
	if parseErr == nil && (params.Get("code") != "" || params.Get("error") != "" || strings.Contains(value, "=")) {
		code, state = params.Get("code"), params.Get("state")
		if oauthErr := params.Get("error"); code == "" && oauthErr != "" {
			return "", "", fmt.Errorf("%w: authorization failed: %s", ErrInvalidCallback, oauthErr)
		}
		if code == "" {
			return "", "", fmt.Errorf("%w: callback url carries no authorization code", ErrInvalidCallback)
		}
		return code, state, nil
	}

	if strings.Contains(value, "://") || strings.Contains(value, "=") {
		return "", "", fmt.Errorf("%w: callback url carries no authorization code", ErrInvalidCallback)
	}
	return value, "", nil
}
