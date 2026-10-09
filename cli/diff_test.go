package cli_test

import (
	"bytes"
	"context"
	"github.com/islandsexit/BltCli/cli"
	"strings"
	"testing"
)

func TestDiffArguments(t *testing.T) {
	for _, args := range [][]string{{"diff"}, {"diff", "one.db"}, {"diff", "a", "b", "c"}, {"tui", "--diff", "a"}, {"tui", "--diff", "--db", "a", "b", "c"}, {"db", "info", "--diff"}} {
		var out, diag bytes.Buffer
		if err := cli.Run(context.Background(), args, strings.NewReader(""), &out, &diag); err == nil {
			t.Fatalf("accepted invalid diff arguments: %v", args)
		}
	}
}
