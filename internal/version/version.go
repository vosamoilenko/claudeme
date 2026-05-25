package version

import "fmt"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func Print() {
	fmt.Printf("claudeme %s (commit: %s, built: %s)\n", Version, Commit, Date)
}
