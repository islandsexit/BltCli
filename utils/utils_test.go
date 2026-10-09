package utils

import (
	"bytes"
	"strings"
	"testing"
)

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
