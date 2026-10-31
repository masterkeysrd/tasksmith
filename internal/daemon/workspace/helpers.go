package workspace

import (
	"fmt"
	"maps"

	"github.com/masterkeysrd/warp"
)

// MergeStringArgs copies string arguments into globals map under both their
// named keys and the "Args" map key.
func MergeStringArgs(args map[string]string, globals map[string]any) map[string]any {
	merged := make(map[string]any)
	maps.Copy(merged, globals)
	for k, v := range args {
		merged[k] = v
	}
	if _, hasArgs := merged["Args"]; !hasArgs && args != nil {
		merged["Args"] = args
	}
	return merged
}

// BuildPositionalArgs maps arguments into an ordered slice according to argsDef.
func BuildPositionalArgs(argsDef []warp.Argument, args map[string]string) []string {
	if len(args) == 0 {
		return nil
	}
	var out []string
	if len(argsDef) > 0 {
		out = make([]string, len(argsDef))
		for i, a := range argsDef {
			if val, ok := args[a.Name]; ok {
				out[i] = val
			} else if val, ok := args[fmt.Sprintf("%d", i+1)]; ok {
				out[i] = val
			} else if a.Default != "" {
				out[i] = a.Default
			}
		}
		return out
	}
	for i := 1; i <= len(args); i++ {
		val, ok := args[fmt.Sprintf("%d", i)]
		if !ok {
			break
		}
		out = append(out, val)
	}
	return out
}

func cloneSlice[T any](s []T) []T {
	if s == nil {
		return nil
	}
	out := make([]T, len(s))
	copy(out, s)
	return out
}
