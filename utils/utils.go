// Package utils provides binary-safe command argument codecs.
package utils

import (
	"bltCli/domain"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
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

// SafeText strips terminal controls from untrusted labels and diagnostics.
func SafeText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
