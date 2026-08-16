package main

import (
	"os"

	"github.com/Pzharyuk/harness-memory/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.Env{}))
}
