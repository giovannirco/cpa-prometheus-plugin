// Command checkzip asserts a packaged release zip matches the CLIProxyAPI
// plugin store layout: one dynamic library, at the zip root, named for the
// target GOOS. CI runs it on every cross-built target.
package main

import (
	"fmt"
	"os"

	"github.com/giovannirco/cpa-prometheus-plugin/internal/release"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: checkzip <zip> <goos>")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := release.ValidatePlatformZip(data, "cpa-prometheus", os.Args[2]); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
	fmt.Printf("%s: zip layout ok for %s\n", os.Args[1], os.Args[2])
}
