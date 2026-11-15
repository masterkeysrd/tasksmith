package daemon

import (
	_ "embed"
	"html/template"
)

//go:embed oauth_callback.html
var oauthCallbackHTML string

var oauthCallbackTemplate = template.Must(template.New("oauth_callback").Parse(oauthCallbackHTML))
