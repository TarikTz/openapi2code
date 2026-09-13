package main

import (
	"os"

	"github.com/tarikomercehajic/openapi2code/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stderr))
}
