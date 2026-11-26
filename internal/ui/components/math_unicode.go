package components

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

var mathSymbols = map[string]string{
	"rightarrow": "→", "to": "→", "leftarrow": "←", "gets": "←", "leftrightarrow": "↔", "Rightarrow": "⇒", "implies": "⇒", "Leftarrow": "⇐", "Leftrightarrow": "⇔", "iff": "⇔", "mapsto": "↦", "uparrow": "↑", "downarrow": "↓", "forall": "∀", "exists": "∃", "neg": "¬", "land": "∧", "lor": "∨",
	"le": "≤", "leq": "≤", "ge": "≥", "geq": "≥", "ne": "≠", "neq": "≠", "approx": "≈", "equiv": "≡", "sim": "∼", "pm": "±", "mp": "∓", "times": "×", "cdot": "·", "div": "÷", "in": "∈", "notin": "∉", "subset": "⊂", "subseteq": "⊆", "supset": "⊃", "supseteq": "⊇", "cup": "∪", "cap": "∩", "emptyset": "∅", "varnothing": "∅", "infty": "∞", "sum": "∑", "prod": "∏", "int": "∫", "partial": "∂", "nabla": "∇", "dots": "…", "ldots": "…", "cdots": "⋯",
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ε", "zeta": "ζ", "eta": "η", "theta": "θ", "iota": "ι", "kappa": "κ", "lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ", "omicron": "ο", "pi": "π", "rho": "ρ", "sigma": "σ", "tau": "τ", "upsilon": "υ", "phi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω", "varepsilon": "ϵ", "vartheta": "ϑ", "varkappa": "ϰ", "varpi": "ϖ", "varrho": "ϱ", "varsigma": "ς", "varphi": "ϕ",
	"Alpha": "Α", "Beta": "Β", "Gamma": "Γ", "Delta": "Δ", "Epsilon": "Ε", "Zeta": "Ζ", "Eta": "Η", "Theta": "Θ", "Iota": "Ι", "Kappa": "Κ", "Lambda": "Λ", "Mu": "Μ", "Nu": "Ν", "Xi": "Ξ", "Omicron": "Ο", "Pi": "Π", "Rho": "Ρ", "Sigma": "Σ", "Tau": "Τ", "Upsilon": "Υ", "Phi": "Φ", "Chi": "Χ", "Psi": "Ψ", "Omega": "Ω",
	"quad": " ", "qquad": "  ", ",": " ", ";": " ", "!": "", " ": " ", "%": "%", "_": "_", "{": "{", "}": "}", "$": "$", "&": "&", "#": "#",
}

type mathStyle uint8

const (
	mathBold mathStyle = 1 << iota
	mathMono
)

type mathSegment struct {
	text  string
	style mathStyle
}

func asciiLetter(b byte) bool { return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' }

func mathGroup(s string) (body, rest string, ok bool) {
	if !strings.HasPrefix(s, "{") {
		return "", s, false
	}
	depth := 1
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[1:i], s[i+1:], true
			}
		}
	}
	return "", s, false
}

func mathScript(s string, super bool) (string, bool) {
	keys := "0123456789+-=()aehijklmnoprstuvx"
	glyphs := "₀₁₂₃₄₅₆₇₈₉₊₋₌₍₎ₐₑₕᵢⱼₖₗₘₙₒₚᵣₛₜᵤᵥₓ"
	if super {
		keys = "0123456789+-=()abcdefghijklmnoprstuvwxyz"
		glyphs = "⁰¹²³⁴⁵⁶⁷⁸⁹⁺⁻⁼⁽⁾ᵃᵇᶜᵈᵉᶠᵍʰⁱʲᵏˡᵐⁿᵒᵖʳˢᵗᵘᵛʷˣʸᶻ"
	}
	table := []rune(glyphs)
	var b strings.Builder
	for _, r := range s {
		idx := strings.IndexRune(keys, r)
		if idx < 0 {
			return "", false
		}
		b.WriteRune(table[idx])
	}
	return b.String(), true
}

func compoundMathOperand(s string) bool {
	return utf8.RuneCountInString(s) > 1 && strings.IndexFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) >= 0
}

func emitMathOperand(body string, style mathStyle, depth int, emit func(string, mathStyle)) {
	var parts []mathSegment
	var text strings.Builder
	parseMath(body, style, depth, func(s string, st mathStyle) {
		text.WriteString(s)
		parts = append(parts, mathSegment{s, st})
	})
	compound := compoundMathOperand(text.String())
	if compound {
		emit("(", style)
	}
	for _, part := range parts {
		emit(part.text, part.style)
	}
	if compound {
		emit(")", style)
	}
}

func translateMath(s string) []mathSegment {
	var out []mathSegment
	parseMath(s, 0, 0, func(text string, style mathStyle) {
		if text != "" {
			out = append(out, mathSegment{text, style})
		}
	})
	return out
}

func parseMath(s string, style mathStyle, depth int, emit func(string, mathStyle)) {
	if depth >= 64 {
		emit(s, style)
		return
	}
	var buf strings.Builder
	flush := func() { emit(buf.String(), style); buf.Reset() }
	plain := func(body string) string {
		var b strings.Builder
		parseMath(body, style, depth+1, func(t string, _ mathStyle) { b.WriteString(t) })
		return b.String()
	}
	for len(s) > 0 {
		switch s[0] {
		case '{':
			body, rest, ok := mathGroup(s)
			if !ok {
				buf.WriteString(s)
				s = ""
				continue
			}
			flush()
			parseMath(body, style, depth+1, emit)
			s = rest
		case '^', '_':
			marker := s[:1]
			s = s[1:]
			body, rest, ok := mathGroup(s)
			if !ok {
				if s == "" || s[0] == '{' {
					buf.WriteString(marker)
					continue
				}
				_, n := utf8.DecodeRuneInString(s)
				body, rest = s[:n], s[n:]
			}
			s = rest
			value := plain(body)
			if converted, ok := mathScript(value, marker == "^"); ok {
				buf.WriteString(converted)
			} else {
				buf.WriteString(marker)
				if utf8.RuneCountInString(value) > 1 {
					value = "(" + value + ")"
				}
				buf.WriteString(value)
			}
		case '\\':
			s = s[1:]
			i := 0
			for i < len(s) && asciiLetter(s[i]) {
				i++
			}
			if i == 0 && len(s) > 0 {
				_, i = utf8.DecodeRuneInString(s)
			}
			name := s[:i]
			s = s[i:]
			if glyph, ok := mathSymbols[name]; ok {
				buf.WriteString(glyph)
				continue
			}
			switch name {
			case "text", "mathrm", "mathbf", "mathtt", "mathcal", "operatorname":
				if body, rest, ok := mathGroup(strings.TrimLeft(s, " \t")); ok {
					next := style
					if name == "mathbf" {
						next |= mathBold
					}
					if name == "mathtt" {
						next |= mathMono
					}
					flush()
					parseMath(body, next, depth+1, emit)
					s = rest
					continue
				}
			case "sqrt", "frac":
				args := strings.TrimLeft(s, " \t")
				index := ""
				if name == "sqrt" && strings.HasPrefix(args, "[") {
					if idx := strings.IndexByte(args, ']'); idx >= 0 {
						index, args = args[1:idx], strings.TrimLeft(args[idx+1:], " \t")
					}
				}
				if a, rest, ok := mathGroup(args); ok {
					if name == "sqrt" {
						if exp, ok := mathScript(index, true); ok {
							buf.WriteString(exp)
						} else {
							buf.WriteString("[" + index + "]")
						}
						buf.WriteRune('√')
						flush()
						emitMathOperand(a, style, depth+1, emit)
						s = rest
						continue
					}
					if b, tail, ok := mathGroup(strings.TrimLeft(rest, " \t")); ok {
						flush()
						emitMathOperand(a, style, depth+1, emit)
						emit("/", style)
						emitMathOperand(b, style, depth+1, emit)
						s = tail
						continue
					}
				}
			case "ln", "log", "exp", "min", "max", "sin", "cos", "tan", "lim", "det", "dim", "arg", "Pr":
				buf.WriteString(name)
				continue
			}
			buf.WriteString(`\` + name)
		default:
			i := strings.IndexAny(s, "{^_\\")
			if i < 0 {
				i = len(s)
			}
			buf.WriteString(s[:i])
			s = s[i:]
		}
	}
	flush()
}
