package buildinfo

import "fmt"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func Summary() string {
	return fmt.Sprintf("runtree %s", Version)
}

func Details() string {
	return fmt.Sprintf("runtree %s\ncommit: %s\nbuilt: %s", Version, Commit, Date)
}
