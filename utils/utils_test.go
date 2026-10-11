package utils

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestValuePreview(t *testing.T) {
	input := []byte(`{"Entries":[{"FixedVersion":"版本","Arches":["aarch64"]}],"empty":{},"literal":"a\\\"b"}`)
	pretty := ValuePreview(input, 64<<10)
	if !strings.Contains(pretty, "\n  \"Entries\": [\n") || !strings.Contains(pretty, "版本") || !json.Valid([]byte(pretty)) {
		t.Fatalf("invalid pretty JSON: %s", pretty)
	}
	var original, formatted any
	if err := json.Unmarshal(input, &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(pretty), &formatted); err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(original)
	b, _ := json.Marshal(formatted)
	if !bytes.Equal(a, b) {
		t.Fatal("formatting changed JSON data")
	}
	yaml := "entries:\n  - version: 1.2\n    arches:\n      - aarch64\n"
	if got := ValuePreview([]byte(yaml), 64<<10); got != yaml {
		t.Fatalf("YAML layout changed: %q", got)
	}
	for _, input := range [][]byte{{0, 255}, []byte("\x1b[31munsafe")} {
		if got := ValuePreview(input, 64<<10); got != Display(input, len(input)) {
			t.Fatalf("unsafe binary preview: %q", got)
		}
	}
	deep := []byte(strings.Repeat("[", 1000) + "0" + strings.Repeat("]", 1000))
	if got := ValuePreview(deep, 4096); len(got) > 4096+len("\n… (preview truncated)") || !strings.Contains(got, "preview truncated") {
		t.Fatal("pretty JSON output is not bounded")
	}
}

func TestBinaryCodecs(t *testing.T) {
	for _, s := range []string{"hex:00ff", "base64:AP8="} {
		got, e := Decode(s)
		if e != nil || !bytes.Equal(got, []byte{0, 255}) {
			t.Fatalf("%s %x %v", s, got, e)
		}
	}
	p, e := ParsePath("users/hex:612f62")
	if e != nil || len(p) != 2 || string(p[1]) != "a/b" {
		t.Fatalf("%q %v", p, e)
	}
	if _, e = ParsePath("a//b"); e == nil {
		t.Fatal("empty component accepted")
	}
	if strings.Contains(Display([]byte{27, '[', '2', 'J'}, 100), "\x1b") {
		t.Fatal("unescaped terminal control")
	}
}
func FuzzDecode(f *testing.F) {
	f.Add("hex:00ff")
	f.Add("base64:AP8=")
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 4096 {
			t.Skip()
		}
		_, _ = Decode(s)
		_, _ = ParsePath(s)
	})
}

func FuzzValuePreview(f *testing.F) {
	f.Add([]byte(`{"ключ":["界",true,null]}`), uint16(128))
	f.Add([]byte{0, 27, 255}, uint16(8))
	f.Add([]byte("name: demo\nitems:\n  - first\n"), uint16(16))
	f.Fuzz(func(t *testing.T, value []byte, requested uint16) {
		if len(value) > 4096 {
			t.Skip()
		}
		limit := 1 + int(requested)%2048
		preview := ValuePreview(value, limit)
		if !utf8.ValidString(preview) {
			t.Fatal("preview split a UTF-8 sequence")
		}
		if strings.ContainsRune(preview, '\x1b') {
			t.Fatal("preview leaked terminal escape")
		}
		if len(preview) > 4*limit+128 {
			t.Fatalf("unbounded preview: %d bytes for limit %d", len(preview), limit)
		}
	})
}
