// Command shed is the spec-driven software factory.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/kpenfound/shed/internal/cli"
)

// version is the release this binary was built from. The release workflow
// sets it with -ldflags "-X main.version=<tag>".
var version = ""

func main() {
	if version != "" {
		cli.Version = version
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
