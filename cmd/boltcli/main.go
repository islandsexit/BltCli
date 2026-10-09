package main

import (
	"context"
	"fmt"
	"github.com/islandsexit/BltCli/cli"
	"os"
	"os/signal"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "boltcli:", err)
		os.Exit(1)
	}
}
