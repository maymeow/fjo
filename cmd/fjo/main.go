// Command fjo is a command-line interface for Forgejo.
package main

import (
	"os"

	"github.com/maymeow/fjo/internal/cli"
)

func main() {
	os.Exit(int(cli.Main(os.Args[1:])))
}
