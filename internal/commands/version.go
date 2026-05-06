package commands

import (
	"fmt"

	"github.com/tinycs-cn/cli/internal/version"
)

func VersionCommand() {
	fmt.Printf("tinycs %s (%s)\n", version.Version, version.Commit)
}
