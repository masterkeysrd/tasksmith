package env

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
)

// Parse reads .env formatted content from r and returns an isolated map of
// key-value pairs. It never reads or mutates the process environment.
func Parse(r io.Reader) (map[string]string, error) {
	if r == nil {
		return map[string]string{}, nil
	}

	result := make(map[string]string)
	scanner := bufio.NewScanner(r)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, rawVal, ok := parseLineKeyValue(line)
		if !ok {
			return nil, fmt.Errorf("env: line %d: missing '=' separator", lineNum)
		}
		if key == "" {
			return nil, fmt.Errorf("env: line %d: empty variable name", lineNum)
		}

		val, err := parseValue(rawVal)
		if err != nil {
			return nil, fmt.Errorf("env: line %d: %w", lineNum, err)
		}

		result[key] = val
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("env: read error: %w", err)
	}

	return result, nil
}

// LoadFile reads and parses a .env file at path. If the file does not exist,
// LoadFile returns an empty non-nil map and a nil error.
func LoadFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("env: open %s: %w", path, err)
	}
	values, parseErr := Parse(f)
	return values, errors.Join(parseErr, f.Close())
}

// Merge combines the provided environment maps in order and returns a new
// isolated map. Keys in later maps override keys in earlier maps.
func Merge(envs ...map[string]string) map[string]string {
	merged := make(map[string]string)
	for _, m := range envs {
		maps.Copy(merged, m)
	}
	return merged
}

// SetInFile sets key=value in the .env file at path. If key already exists in
// the file, its line is updated in-place while preserving all comments, blank
// lines, and key ordering. Otherwise, key=value is appended at the end.
//
// Parent directories are created as needed. Newly created files are written
// with 0600 permissions; existing files preserve their current permissions.
func SetInFile(path, key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("env: key must not be empty")
	}
	if strings.ContainsAny(key, "=\r\n") {
		return fmt.Errorf("env: invalid character in key %q", key)
	}

	perm := os.FileMode(0o600)
	var existingContent []byte

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("env: create directory %s: %w", dir, err)
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(path)
	info, err := root.Stat(name)
	if err == nil {
		perm = info.Mode().Perm()
		existingContent, err = root.ReadFile(name)
		if err != nil {
			return fmt.Errorf("env: read %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("env: stat %s: %w", path, err)
	}

	formattedLine := key + "=" + formatValue(value)

	var outLines []string
	if len(existingContent) > 0 {
		text := strings.ReplaceAll(string(existingContent), "\r\n", "\n")
		text = strings.TrimSuffix(text, "\n")
		lines := strings.Split(text, "\n")

		updated := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				if lineKey, _, ok := parseLineKeyValue(trimmed); ok && lineKey == key {
					if !updated {
						outLines = append(outLines, formattedLine)
						updated = true
					}
					continue
				}
			}
			outLines = append(outLines, line)
		}

		if !updated {
			outLines = append(outLines, formattedLine)
		}
	} else {
		outLines = []string{formattedLine}
	}

	output := strings.Join(outLines, "\n") + "\n"

	return fsutils.WriteFileAtomicInRoot(root, name, []byte(output), perm)
}

// parseLineKeyValue splits a non-empty, non-comment line into its trimmed key
// (stripping any leading "export " prefix) and trimmed raw value.
func parseLineKeyValue(line string) (key, rawVal string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "export") && len(trimmed) > len("export") {
		next := trimmed[len("export")]
		if next == ' ' || next == '\t' {
			trimmed = strings.TrimSpace(trimmed[len("export"):])
		}
	}

	eqIdx := strings.IndexByte(trimmed, '=')
	if eqIdx < 0 {
		return "", "", false
	}

	key = strings.TrimSpace(trimmed[:eqIdx])
	rawVal = strings.TrimSpace(trimmed[eqIdx+1:])
	return key, rawVal, true
}

// parseValue parses a trimmed raw value from a .env line, stripping matching
// surrounding single or double quotes, expanding basic escapes inside double
// quotes, and stripping inline # comments on unquoted values.
func parseValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}

	switch raw[0] {
	case '\'':
		closeIdx := strings.IndexByte(raw[1:], '\'')
		if closeIdx < 0 {
			return "", errors.New("unterminated single-quoted value")
		}
		closeIdx++ // adjust for slice offset
		trailing := strings.TrimSpace(raw[closeIdx+1:])
		if trailing != "" && !strings.HasPrefix(trailing, "#") {
			return "", fmt.Errorf("unexpected trailing characters after single-quoted value: %q", trailing)
		}
		return raw[1:closeIdx], nil

	case '"':
		var b strings.Builder
		escaped := false
		closeIdx := -1
		for i := 1; i < len(raw); i++ {
			ch := raw[i]
			if escaped {
				switch ch {
				case '"':
					b.WriteByte('"')
				case '\\':
					b.WriteByte('\\')
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte('\\')
					b.WriteByte(ch)
				}
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				closeIdx = i
				break
			}
			b.WriteByte(ch)
		}
		if closeIdx < 0 {
			return "", errors.New("unterminated double-quoted value")
		}
		trailing := strings.TrimSpace(raw[closeIdx+1:])
		if trailing != "" && !strings.HasPrefix(trailing, "#") {
			return "", fmt.Errorf("unexpected trailing characters after double-quoted value: %q", trailing)
		}
		return b.String(), nil

	default:
		if hashIdx := strings.IndexByte(raw, '#'); hashIdx >= 0 {
			raw = raw[:hashIdx]
		}
		return strings.TrimSpace(raw), nil
	}
}

// formatValue formats value for writing to a .env file, automatically wrapping
// it in double quotes with escaped characters if it contains spaces, '#',
// quotes, or newlines.
func formatValue(value string) string {
	needsQuotes := false
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case ' ', '\t', '#', '"', '\'', '\n', '\r':
			needsQuotes = true
		}
		if needsQuotes {
			break
		}
	}

	if !needsQuotes {
		return value
	}

	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteByte(value[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}
