package permissions

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

// ParseRule parses a permission rule string in "tool(pattern)" format into its
// constituent tool name and pattern components.
func ParseRule(raw string) (toolName, pattern string, err error) {
	raw = strings.TrimSpace(raw)
	openIdx := strings.Index(raw, "(")
	if openIdx < 0 || !strings.HasSuffix(raw, ")") {
		return "", "", fmt.Errorf("%w: rule %q must be in tool(pattern) format", ErrInvalidRule, raw)
	}

	toolName = raw[:openIdx]
	if toolName == "" || strings.ContainsAny(toolName, " \t\n\r()") {
		return "", "", fmt.Errorf("%w: invalid tool name in %q", ErrInvalidRule, raw)
	}
	if strings.Index(raw, ")") != len(raw)-1 || strings.Count(raw, "(") != 1 {
		return "", "", fmt.Errorf("%w: invalid rule format %q", ErrInvalidRule, raw)
	}
	pattern = strings.TrimSpace(raw[openIdx+1 : len(raw)-1])
	if pattern == "" || strings.ContainsAny(pattern, "()") {
		return "", "", fmt.Errorf("%w: invalid pattern in %q", ErrInvalidRule, raw)
	}

	return toolName, pattern, nil
}

// FormatRule builds a normalized "tool(pattern)" rule string, defaulting an empty
// pattern to "*".
func FormatRule(toolName, pattern string) string {
	toolName = strings.TrimSpace(toolName)
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		pattern = "*"
	}
	return fmt.Sprintf("%s(%s)", toolName, pattern)
}

// MatchRule checks whether rule matches the planned tool invocation.
func MatchRule(rule string, plan *tools.Plan) bool {
	if plan == nil {
		return false
	}
	toolName, pattern, err := ParseRule(rule)
	if err != nil {
		return false
	}

	if toolName != "*" && !matchPattern(toolName, plan.ToolName) {
		return false
	}

	return matchTarget(pattern, plan.Target)
}

func matchTarget(pattern, target string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "*" {
		return true
	}
	if pattern == target {
		return true
	}

	// URL host matching when url.Parse(target) has a non-empty u.Host.
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		if pattern == u.Host || pattern == u.Hostname() || matchPattern(pattern, u.Host) || matchPattern(pattern, u.Hostname()) {
			return true
		}
	}

	// Directory prefix /** glob matching.
	if before, ok := strings.CutSuffix(pattern, "/**"); ok {
		prefix := strings.TrimSuffix(before, "/")
		if prefix == "" || prefix == "." {
			return true
		}
		if target == prefix || strings.HasPrefix(target, prefix+"/") {
			return true
		}
	}

	// Command / string prefix * matching (without **).
	if strings.HasSuffix(pattern, "*") && !strings.Contains(pattern, "**") {
		prefix := strings.TrimSuffix(pattern, "*")
		if strings.HasPrefix(target, prefix) || strings.TrimSpace(prefix) == target {
			return true
		}
	}

	return matchPattern(pattern, target)
}

func matchPattern(pattern, target string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "*" {
		return true
	}
	if pattern == target {
		return true
	}
	if ok, err := path.Match(pattern, target); err == nil && ok {
		return true
	}
	reStr := globToRegex(pattern)
	if re, err := regexp.Compile(reStr); err == nil {
		return re.MatchString(target)
	}
	return false
}

func globToRegex(glob string) string {
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(glob); i++ {
		ch := glob[i]
		switch ch {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				sb.WriteString(".*")
				i++
				if i+1 < len(glob) && glob[i+1] == '/' {
					i++
				}
			} else {
				sb.WriteString("[^/]*")
			}
		case '?':
			sb.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(ch)
		default:
			sb.WriteByte(ch)
		}
	}
	sb.WriteString("$")
	return sb.String()
}

// DeriveGrantRules generates candidate "tool(pattern)" permission rules
// suitable for PermissionRequest.GrantPatterns based on action and target.
func DeriveGrantRules(toolName string, action tools.ActionType, target string) []string {
	target = strings.TrimSpace(target)
	if target == "" {
		return []string{FormatRule(toolName, "*")}
	}

	switch action {
	case tools.ActionRead, tools.ActionWrite, tools.ActionDelete:
		rules := []string{FormatRule(toolName, target)}
		dir := path.Dir(target)
		if dir != "." && dir != "" && dir != "/" {
			rules = append(rules, FormatRule(toolName, dir+"/**"))
		}
		return deduplicateRules(rules)
	case tools.ActionExec:
		fields := strings.Fields(target)
		if len(fields) >= 2 && !strings.HasPrefix(fields[1], "-") {
			return deduplicateRules([]string{
				FormatRule(toolName, fields[0]+" "+fields[1]+" *"),
				FormatRule(toolName, fields[0]+" *"),
			})
		}
		if len(fields) > 0 {
			return []string{FormatRule(toolName, fields[0]+" *")}
		}
		return []string{FormatRule(toolName, "*")}
	case tools.ActionNet:
		if u, err := url.Parse(target); err == nil && u.Host != "" {
			return deduplicateRules([]string{
				FormatRule(toolName, u.Host),
				FormatRule(toolName, target),
			})
		}
		return []string{FormatRule(toolName, target)}
	default:
		return []string{FormatRule(toolName, target)}
	}
}
