package mcp

import (
	_ "embed"
	"html"
	"io"
	"net/http"
	"strings"
)

// callbackLogoSVG is the TaskSmith anvil mark shipped in the repository's assets
// directory. It is mirrored here because go:embed cannot reach above the package
// directory, and the callback page must be self-contained: the browser reaching
// it may have no network route to the daemon's host at all.
//
//go:embed assets/tasksmith-logo.svg
var callbackLogoSVG string

// Callback page palette. These are TaskSmith's terminal theme colours, restated as
// CSS so the browser page reads as the same product as the terminal it came from.
const (
	callbackInk          = "#c0caf5"
	callbackInkSecondary = "#9aa5ce"
	callbackSurface      = "#16161e"
	callbackBackdrop     = "#1a1b26"
	callbackCyan         = "#2ac3de"
	callbackBlue         = "#7aa2f7"
	callbackSuccess      = "#9ece6a"
	callbackSuccessDeep  = "#1abc9c"
	callbackFailure      = "#ff5f5f"
	callbackFailureDeep  = "#f7768e"
)

// writeCallbackPage serves a branded callback page. The body is written with the
// caller's status code so an authorization failure reads as one to the browser and
// any proxy in between.
func writeCallbackPage(w http.ResponseWriter, status int, page string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, page)
}

// callbackPage carries everything the branded callback page needs to render. The
// zero value renders the success state.
type callbackPage struct {
	failed   bool
	server   string
	code     string
	descript string
}

// callbackSuccessHTML renders the page shown when the identity provider delivered
// an authorization code to the daemon's loopback listener.
func callbackSuccessHTML(serverName string) string {
	return renderCallbackPage(callbackPage{server: serverName})
}

// callbackErrorHTML renders the page shown when the provider reported a failure,
// typically because the user denied the consent screen.
func callbackErrorHTML(serverName, code, description string) string {
	return renderCallbackPage(callbackPage{
		failed:   true,
		server:   serverName,
		code:     code,
		descript: description,
	})
}

// renderCallbackPage builds a self-contained document: no scripts, stylesheets, or
// images are fetched, so it renders identically offline and leaks nothing about the
// local environment.
func renderCallbackPage(page callbackPage) string {
	badge, heading, status := "\u2714", "Authentication Complete", "ok"
	accent, accentDeep := callbackSuccess, callbackSuccessDeep
	if page.failed {
		badge, heading, status = "\u2715", "Authentication Failed", "fail"
		accent, accentDeep = callbackFailure, callbackFailureDeep
	}

	server := strings.TrimSpace(page.server)
	if server == "" {
		server = "MCP server"
	}
	server = html.EscapeString(server)

	message := "Connected <strong>" + server + "</strong> to TaskSmith. You can close this tab and return to your terminal."
	details := ""
	if page.failed {
		message = "TaskSmith could not connect <strong>" + server + "</strong>."
		details = callbackFailureDetails(page.code, page.descript)
	}

	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark light">
<title>TaskSmith · ` + heading + `</title>
<link rel="icon" type="image/svg+xml" href="data:image/svg+xml,` + dataURISVG(callbackLogoSVG) + `">
<style>
:root {
  --ink: ` + callbackInk + `;
  --ink-secondary: ` + callbackInkSecondary + `;
  --surface: ` + callbackSurface + `;
  --backdrop: ` + callbackBackdrop + `;
  --accent: ` + callbackCyan + `;
  --accent-alt: ` + callbackBlue + `;
  --status: ` + accent + `;
  --status-deep: ` + accentDeep + `;
  --line: rgba(192, 202, 245, 0.14);
}
@media (prefers-color-scheme: light) {
  :root {
    --ink: #3b4261;
    --ink-secondary: #4c5373;
    --surface: #ffffff;
    --backdrop: #e8eaf2;
    --line: rgba(59, 66, 97, 0.16);
  }
}
* { box-sizing: border-box; }
html, body { height: 100%; margin: 0; }
body {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: ` + callbackBackdrop + `;
  background: var(--backdrop);
  color: var(--ink);
  font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  line-height: 1.55;
}
.card {
  width: 100%;
  max-width: 460px;
  padding: 36px 34px 30px;
  text-align: center;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 20px;
  box-shadow: 0 18px 48px rgba(0, 0, 0, 0.35);
}
.logo { display: flex; justify-content: center; margin-bottom: 22px; }
.logo svg { height: 76px; width: auto; }
.badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 46px;
  height: 46px;
  margin-bottom: 16px;
  border-radius: 50%;
  color: var(--surface);
  background: linear-gradient(135deg, var(--status), var(--status-deep));
  font-size: 22px;
  font-weight: 700;
}
h1 {
  margin: 0 0 10px;
  font-size: 21px;
  letter-spacing: 0.2px;
}
p { margin: 0 auto; max-width: 38ch; color: var(--ink-secondary); font-size: 15px; }
strong { color: var(--ink); font-weight: 600; }
.details {
  margin: 18px 0 0;
  padding: 12px 14px;
  text-align: left;
  background: rgba(192, 202, 245, 0.06);
  border: 1px solid var(--line);
  border-radius: 12px;
}
.details code {
  display: block;
  color: var(--status);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 13px;
  overflow-wrap: anywhere;
}
.details span {
  display: block;
  margin-top: 6px;
  color: var(--ink-secondary);
  font-size: 13px;
  overflow-wrap: anywhere;
}
button {
  margin-top: 24px;
  padding: 10px 22px;
  color: var(--surface);
  background: linear-gradient(135deg, var(--accent), var(--accent-alt));
  border: 0;
  border-radius: 999px;
  font: inherit;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
}
button:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
.hint { margin-top: 16px; font-size: 13px; }
</style>
</head>
<body>
<main class="card">
  <div class="logo">` + callbackLogoSVG + `</div>
  <div class="badge">` + badge + `</div>
  <h1>` + heading + `</h1>
  <p>` + message + `</p>` + details + `
  <button type="button" onclick="window.close()">Close Window</button>
  <p class="hint" data-status="` + status + `">Return to TaskSmith in your terminal to continue.</p>
</main>
</body>
</html>
`
}

// callbackFailureDetails renders the OAuth error block. Both values come from the
// redirect query string, so they are escaped rather than trusted as markup.
func callbackFailureDetails(code, description string) string {
	code = html.EscapeString(strings.TrimSpace(code))
	description = html.EscapeString(strings.TrimSpace(description))
	if code == "" && description == "" {
		return ""
	}
	block := "\n  <div class=\"details\">"
	if code != "" {
		block += "<code>" + code + "</code>"
	}
	if description != "" {
		block += "<span>" + description + "</span>"
	}
	return block + "</div>"
}

// dataURISVG converts an SVG document into a data URI fragment. Only the characters
// that would break an HTML attribute value are rewritten, which keeps the markup
// readable in the browser's address bar and avoids base64 bloat.
func dataURISVG(svg string) string {
	out := strings.ReplaceAll(svg, "\n", " ")
	out = strings.ReplaceAll(out, "\"", "'")
	out = strings.ReplaceAll(out, "#", "%23")
	out = strings.ReplaceAll(out, "<", "%3C")
	out = strings.ReplaceAll(out, ">", "%3E")
	return out
}
