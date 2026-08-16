// Command mineguard is the MineGuard entry point.
package main

import (
	"os"

	"MineGuard/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
