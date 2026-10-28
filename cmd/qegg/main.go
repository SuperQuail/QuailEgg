package main

import (
	"os"

	"github.com/SuperQuail/QuailEgg/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:]))
}
