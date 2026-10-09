package config

import (
	"bytes"
	"testing"
)

func TestFlagsAndQuery(t *testing.T) {
	c, e := Parse([]string{"--db", "example.db", "--bucket", "users/hex:00ff", "--prefix", "hex:ff", "--after", "base64:AP8=", "--limit", "20", "--timeout", "2s", "key"}, &bytes.Buffer{})
	if e != nil {
		t.Fatal(e)
	}
	p, e := c.Path()
	if e != nil || len(p) != 2 || !bytes.Equal(p[1], []byte{0, 255}) {
		t.Fatalf("%q %v", p, e)
	}
	q, e := c.Query()
	if e != nil || q.Limit != 20 || !bytes.Equal(q.Prefix, []byte{255}) || !bytes.Equal(q.After, []byte{0, 255}) {
		t.Fatalf("%+v %v", q, e)
	}
	if len(c.Args) != 1 || c.Args[0] != "key" {
		t.Fatal(c.Args)
	}
}
func TestInvalidLimits(t *testing.T) {
	for _, args := range [][]string{{"--limit", "0"}, {"--limit", "10001"}, {"--batch-size", "0"}, {"--batch-bytes", "0"}, {"--max-record-bytes", "0"}, {"--timeout", "-1s"}, {"--unknown"}} {
		if _, e := Parse(args, &bytes.Buffer{}); e == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
func FuzzFlags(f *testing.F) {
	f.Add("--limit", "100")
	f.Add("--prefix", "hex:00")
	f.Fuzz(func(t *testing.T, flag, value string) {
		if len(flag)+len(value) > 4096 {
			t.Skip()
		}
		c, e := Parse([]string{flag, value}, &bytes.Buffer{})
		if e == nil {
			_, _ = c.Path()
			_, _ = c.Query()
		}
	})
}
