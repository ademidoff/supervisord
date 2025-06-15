package main

import (
	"fmt"
)

// VERSION the version of supervisor - injected at build time
// COMMIT the git commit hash - injected at build time

var (
	BuildVersion = "dev"
	BuildCommit  = "unknown"
)

// VersionCommand implement the flags.Commander interface
type VersionCommand struct {
}

var versionCommand VersionCommand

// Execute implement Execute() method defined in flags.Commander interface, executes the given command
func (v VersionCommand) Execute(args []string) error {
	fmt.Println("Version:", BuildVersion)
	fmt.Println("Commit:", BuildCommit)
	return nil
}

func init() {
	parser.AddCommand("version",
		"display the version of supervisor",
		"This command displays the version and commit hash of the supervisor",
		&versionCommand)
}
