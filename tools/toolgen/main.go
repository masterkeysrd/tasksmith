// Command toolgen generates strongly typed Go input/output structs and tool
// name constants from WARP *.tool.md manifests.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/masterkeysrd/warp"
)

type parsedTool struct {
	Name         string
	ConstName    string
	PascalName   string
	InputStruct  structDef
	OutputStruct structDef
	Nested       []structDef
}

type structDef struct {
	Name    string
	Comment string
	Fields  []fieldDef
}

type fieldDef struct {
	Name        string
	GoType      string
	JSONName    string
	Required    bool
	Description string
}

type schemaNode struct {
	Type                 string
	Format               string
	Title                string
	Description          string
	Required             []string
	Properties           []namedSchemaProp
	Items                *schemaNode
	AdditionalProperties *schemaNode
}

type namedSchemaProp struct {
	Name   string
	Schema schemaNode
}

func main() {
	dirFlag := flag.String("dir", ".", "directory containing *.tool.md files")
	pkgFlag := flag.String("pkg", "builtin", "target Go package name")
	outFlag := flag.String("out", "generated.go", "output Go file path")
	flag.Parse()

	entries, err := os.ReadDir(*dirFlag)
	if err != nil {
		fatalf("read dir %q: %v", *dirFlag, err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".tool.md") {
			files = append(files, filepath.Join(*dirFlag, e.Name()))
		}
	}
	sort.Strings(files)

	if len(files) == 0 {
		fatalf("no *.tool.md files found in %q", *dirFlag)
	}

	var tools []parsedTool
	for _, f := range files {
		pt, err := parseToolFile(f)
		if err != nil {
			fatalf("parse %q: %v", f, err)
		}
		tools = append(tools, pt)
	}

	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})

	src, err := renderCode(*pkgFlag, tools)
	if err != nil {
		fatalf("render code: %v", err)
	}

	outPath := *outFlag
	if !filepath.IsAbs(outPath) {
		outPath = filepath.Join(*dirFlag, outPath)
	}
	if err := os.WriteFile(outPath, src, 0o644); err != nil {
		fatalf("write %q: %v", outPath, err)
	}
}

func fatalf(formatStr string, args ...any) {
	fmt.Fprintf(os.Stderr, "toolgen: "+formatStr+"\n", args...)
	os.Exit(1)
}

func parseToolFile(path string) (parsedTool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return parsedTool{}, err
	}

	res, err := warp.Parse(filepath.Base(path), string(raw))
	if err != nil {
		return parsedTool{}, fmt.Errorf("warp.Parse: %w", err)
	}
	wt, ok := res.Resource.(*warp.Tool)
	if !ok || wt == nil {
		return parsedTool{}, fmt.Errorf("expected *warp.Tool resource, got %T", res.Resource)
	}

	name := strings.TrimSpace(wt.Metadata.Name)
	if name == "" {
		return parsedTool{}, fmt.Errorf("metadata.name is required")
	}

	pascal := toPascal(name)
	constName := "Tool" + pascal
	if name == "ls" {
		constName = "ToolLS"
	}

	var nested []structDef
	seenNested := make(map[string]bool)

	inSchema := parseSchemaMap(wt.Spec.InputSchema)
	inStruct := buildStructDef(
		pascal+"Input",
		fmt.Sprintf("%sInput defines the input parameters for the %s tool.", pascal, name),
		pascal,
		inSchema,
		&nested,
		seenNested,
	)

	outSchema := parseSchemaMap(wt.Spec.OutputSchema)
	outStruct := buildStructDef(
		pascal+"Output",
		fmt.Sprintf("%sOutput defines the structured output of the %s tool.", pascal, name),
		pascal,
		outSchema,
		&nested,
		seenNested,
	)

	return parsedTool{
		Name:         name,
		ConstName:    constName,
		PascalName:   pascal,
		InputStruct:  inStruct,
		OutputStruct: outStruct,
		Nested:       nested,
	}, nil
}

func parseSchemaMap(m map[string]any) schemaNode {
	if len(m) == 0 {
		return schemaNode{Type: "object"}
	}

	sn := schemaNode{
		Type:        stringVal(m["type"]),
		Format:      stringVal(m["format"]),
		Title:       stringVal(m["title"]),
		Description: stringVal(m["description"]),
	}

	switch req := m["required"].(type) {
	case []any:
		for _, item := range req {
			if s := stringVal(item); s != "" {
				sn.Required = append(sn.Required, s)
			}
		}
	case []string:
		for _, s := range req {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				sn.Required = append(sn.Required, trimmed)
			}
		}
	}

	if props, ok := m["properties"].(map[string]any); ok {
		propKeys := make([]string, 0, len(props))
		for k := range props {
			propKeys = append(propKeys, k)
		}
		sort.Slice(propKeys, func(i, j int) bool {
			reqI := slices.Index(sn.Required, propKeys[i])
			reqJ := slices.Index(sn.Required, propKeys[j])
			if reqI >= 0 && reqJ >= 0 {
				return reqI < reqJ
			}
			if reqI >= 0 {
				return true
			}
			if reqJ >= 0 {
				return false
			}
			return propKeys[i] < propKeys[j]
		})
		for _, k := range propKeys {
			if propMap, ok := props[k].(map[string]any); ok {
				sn.Properties = append(sn.Properties, namedSchemaProp{
					Name:   k,
					Schema: parseSchemaMap(propMap),
				})
			}
		}
	}

	if itemsMap, ok := m["items"].(map[string]any); ok {
		child := parseSchemaMap(itemsMap)
		sn.Items = &child
	}

	switch addProps := m["additionalProperties"].(type) {
	case map[string]any:
		child := parseSchemaMap(addProps)
		sn.AdditionalProperties = &child
	case bool:
		if addProps {
			sn.AdditionalProperties = &schemaNode{}
		}
	}

	return sn
}

func stringVal(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func buildStructDef(structName, comment, toolPascal string, sn schemaNode, nested *[]structDef, seen map[string]bool) structDef {
	sd := structDef{
		Name:    structName,
		Comment: comment,
	}

	for _, prop := range sn.Properties {
		req := slices.Contains(sn.Required, prop.Name)
		goType := resolveGoType(toolPascal, prop.Name, prop.Schema, nested, seen)
		sd.Fields = append(sd.Fields, fieldDef{
			Name:        toFieldPascal(prop.Name),
			GoType:      goType,
			JSONName:    prop.Name,
			Required:    req,
			Description: sanitizeDescription(prop.Schema.Description),
		})
	}
	return sd
}

func resolveGoType(toolPascal, propName string, sn schemaNode, nested *[]structDef, seen map[string]bool) string {
	switch sn.Type {
	case "string":
		return "string"
	case "boolean":
		return "bool"
	case "integer":
		if sn.Format == "int64" {
			return "int64"
		}
		if sn.Format == "uint32" {
			return "uint32"
		}
		return "int"
	case "number":
		return "float64"
	case "array":
		if sn.Items == nil {
			return "[]any"
		}
		if sn.Items.Type == "object" && len(sn.Items.Properties) > 0 {
			nestedName := sn.Items.Title
			if nestedName == "" {
				nestedName = toolPascal + toPascal(singularize(propName))
			}
			if !seen[nestedName] {
				seen[nestedName] = true
				nsd := buildStructDef(
					nestedName,
					fmt.Sprintf("%s represents an item in %s.", nestedName, propName),
					toolPascal,
					*sn.Items,
					nested,
					seen,
				)
				*nested = append(*nested, nsd)
			}
			return "[]" + nestedName
		}
		elemType := resolveGoType(toolPascal, singularize(propName), *sn.Items, nested, seen)
		return "[]" + elemType
	case "object":
		if len(sn.Properties) > 0 {
			nestedName := sn.Title
			if nestedName == "" {
				nestedName = toolPascal + toPascal(propName)
			}
			if !seen[nestedName] {
				seen[nestedName] = true
				nsd := buildStructDef(
					nestedName,
					fmt.Sprintf("%s represents the %s object.", nestedName, propName),
					toolPascal,
					sn,
					nested,
					seen,
				)
				*nested = append(*nested, nsd)
			}
			return nestedName
		}
		if sn.AdditionalProperties != nil && sn.AdditionalProperties.Type != "" {
			valType := resolveGoType(toolPascal, propName, *sn.AdditionalProperties, nested, seen)
			return "map[string]" + valType
		}
		return "map[string]any"
	default:
		return "any"
	}
}

func sanitizeDescription(desc string) string {
	desc = strings.ReplaceAll(desc, "\n", " ")
	desc = strings.TrimSpace(desc)
	if fields := strings.Fields(desc); len(fields) > 0 && strings.Contains(fields[0], "=") {
		desc = " Value: " + desc
	}
	return desc
}

var knownAcronyms = map[string]string{
	"id":  "ID",
	"url": "URL",
}

func toPascal(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	var sb strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		lower := strings.ToLower(p)
		sb.WriteString(strings.ToUpper(lower[:1]))
		sb.WriteString(lower[1:])
	}
	return sb.String()
}

func toFieldPascal(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	var sb strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		lower := strings.ToLower(p)
		if acr, ok := knownAcronyms[lower]; ok {
			sb.WriteString(acr)
		} else {
			sb.WriteString(strings.ToUpper(lower[:1]))
			sb.WriteString(lower[1:])
		}
	}
	return sb.String()
}

func singularize(word string) string {
	lower := strings.ToLower(word)
	switch {
	case strings.HasSuffix(lower, "ies") && len(word) > 3:
		return word[:len(word)-3] + "y"
	case strings.HasSuffix(lower, "ches") || strings.HasSuffix(lower, "shes") || strings.HasSuffix(lower, "xes") || strings.HasSuffix(lower, "zes") || strings.HasSuffix(lower, "sses"):
		return word[:len(word)-2]
	case strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ss") && !strings.HasSuffix(lower, "us") && len(word) > 1:
		return word[:len(word)-1]
	default:
		return word
	}
}

func renderCode(pkg string, tools []parsedTool) ([]byte, error) {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "// Code generated by toolgen; DO NOT EDIT.\n\n")
	fmt.Fprintf(&buf, "package %s\n\n", pkg)

	fmt.Fprintf(&buf, "// Built-in system tool identifiers.\n")
	fmt.Fprintf(&buf, "const (\n")
	for _, t := range tools {
		fmt.Fprintf(&buf, "\t%s = %q\n", t.ConstName, t.Name)
	}
	fmt.Fprintf(&buf, ")\n\n")

	for _, t := range tools {
		for _, ns := range t.Nested {
			writeStruct(&buf, ns)
		}
		writeStruct(&buf, t.InputStruct)
		writeStruct(&buf, t.OutputStruct)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("gofmt generated source: %w\n%s", err, buf.String())
	}
	return formatted, nil
}

func writeStruct(buf *bytes.Buffer, sd structDef) {
	if sd.Comment != "" {
		fmt.Fprintf(buf, "// %s\n", sd.Comment)
	}
	fmt.Fprintf(buf, "type %s struct {\n", sd.Name)
	for _, f := range sd.Fields {
		jsonTag := f.JSONName
		if !f.Required {
			jsonTag += ",omitempty"
		}
		if f.Description != "" {
			fmt.Fprintf(buf, "\t%s %s `json:%q jsonschema:%q`\n", f.Name, f.GoType, jsonTag, f.Description)
		} else {
			fmt.Fprintf(buf, "\t%s %s `json:%q`\n", f.Name, f.GoType, jsonTag)
		}
	}
	fmt.Fprintf(buf, "}\n\n")
}
