package main

import (
	"strings"
	"testing"
)

func TestExplicitOptionalBooleanPointer(t *testing.T) {
	schema := parseSchemaMap(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"confirmed": map[string]any{"type": "boolean", "x-go-pointer": true},
			"ordinary":  map[string]any{"type": "boolean"},
		},
	})
	var nested []structDef
	definition := buildStructDef("Answer", "", "Ask", schema, &nested, make(map[string]bool))
	output, err := renderTypes("api", []parsedTool{{InputStruct: definition, OutputStruct: structDef{Name: "Output"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "Confirmed *bool `json:\"confirmed,omitempty\"`") || !strings.Contains(string(output), "Ordinary  bool  `json:\"ordinary,omitempty\"`") {
		t.Fatalf("unexpected generated fields: %s", output)
	}
}
