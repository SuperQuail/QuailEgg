package main

import (
	"os"

	"github.com/SuperQuail/QuailEgg/internal/buildinfo"
	"github.com/SuperQuail/QuailEgg/internal/cli"
)

// version is set through -ldflags -X by the release build.
var version = ""

func main() {
	buildinfo.SetVersion(version)
	os.Exit(cli.Execute(os.Args[1:]))
}
