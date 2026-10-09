// Package utils provides binary-safe command argument codecs.
package utils

import (
	"bltCli/domain"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Decode accepts plain text or explicit text:, hex:, and base64: prefixes.
func Decode(s string) ([]byte, error) {
	switch {
	case strings.HasPrefix(s, "hex:"):
		return hex.DecodeString(s[4:])
	case strings.HasPrefix(s, "base64:"):
		return base64.StdEncoding.DecodeString(s[7:])
	case strings.HasPrefix(s, "text:"):
		return []byte(s[5:]), nil
	default:
		return []byte(s), nil
	}
}

// ParsePath decodes slash-separated bucket components. Encode literal slashes
// inside a component as hex or base64.
func ParsePath(s string) (domain.Path, error) {
	if s == "" {
		return nil, nil
	}
	var out domain.Path
	for _, part := range strings.Split(s, "/") {
		b, e := Decode(part)
		if e != nil {
			return nil, e
		}
		if len(b) == 0 {
			return nil, fmt.Errorf("empty bucket component")
		}
		out = append(out, b)
	}
	return out, nil
}

// Display escapes control characters and limits previews to a fixed byte count.
func Display(b []byte, max int) string {
	suffix := ""
	if len(b) > max {
		b = b[:max]
		suffix = "…"
	}
	return fmt.Sprintf("%q%s", b, suffix)
}

// ValuePreview renders bounded JSON or multiline text (including YAML).
// Binary data and terminal controls retain the escaped representation.
func ValuePreview(value []byte, limit int) string {
	if limit < 1 {
		return ""
	}
	truncated := len(value) > limit
	if truncated {
		value = value[:limit]
		for len(value) > 0 && !utf8.Valid(value) && len(value) > limit-utf8.UTFMax {
			value = value[:len(value)-1]
		}
	}
	if !utf8.Valid(value) {
		return Display(value, len(value)) + previewSuffix(truncated)
	}
	for _, r := range string(value) {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return Display(value, len(value)) + previewSuffix(truncated)
		}
	}
	if !truncated && json.Valid(value) {
		return prettyJSON(value, limit)
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(value), "\r\n", "\n"), "\t", "    ") + previewSuffix(truncated)
}

func previewSuffix(truncated bool) string {
	if truncated {
		return "\n… (preview truncated)"
	}
	return ""
}

// prettyJSON bounds output as well as input, even for deeply nested JSON.
func prettyJSON(value []byte, limit int) string {
	var out strings.Builder
	depth := 0
	quoted, escaped, truncated := false, false, false
	write := func(s string) {
		if len(s) > limit-out.Len() {
			truncated = true
			return
		}
		out.WriteString(s)
	}
	newline := func() { write("\n" + strings.Repeat("  ", min(depth, 32))) }
	for i, c := range value {
		if quoted {
			write(string(value[i : i+1]))
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
		} else {
			switch c {
			case '"':
				quoted = true
				write("\"")
			case '{', '[':
				write(string(value[i : i+1]))
				depth++
				next := i + 1
				for next < len(value) && (value[next] == ' ' || value[next] == '\n' || value[next] == '\r' || value[next] == '\t') {
					next++
				}
				if next < len(value) && value[next] != '}' && value[next] != ']' {
					newline()
				}
			case '}', ']':
				depth--
				previous := i - 1
				for previous >= 0 && (value[previous] == ' ' || value[previous] == '\n' || value[previous] == '\r' || value[previous] == '\t') {
					previous--
				}
				if previous >= 0 && value[previous] != '{' && value[previous] != '[' {
					newline()
				}
				write(string(value[i : i+1]))
			case ',':
				write(",")
				newline()
			case ':':
				write(": ")
			case ' ', '\n', '\r', '\t':
			default:
				write(string(value[i : i+1]))
			}
		}
		if truncated {
			break
		}
	}
	text := out.String()
	for len(text) > 0 && !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text + previewSuffix(truncated)
}

// SafeText strips terminal controls from untrusted labels and diagnostics.
func SafeText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
