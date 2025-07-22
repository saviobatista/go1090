package app

import "fmt"

// Version information (set by build flags)
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// ShowVersion displays version information
func ShowVersion() {
	fmt.Printf("Go1090 ADS-B Decoder (dump1090-style)\n")
	fmt.Printf("Version: %s\n", Version)
	fmt.Printf("Build Time: %s\n", BuildTime)
	fmt.Printf("Git Commit: %s\n", GitCommit)
}
