package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMakePackageUsesNativeTarget(t *testing.T) {
	root := repoRoot(t)
	dir, err := os.MkdirTemp(root, ".package-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cmd := exec.Command("make", "VERSION=0.0.0-test", "DIST="+filepath.Base(dir), "package")
	cmd.Dir = root
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GOOS=") && !strings.HasPrefix(entry, "GOARCH=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make package: %v\n%s", err, output)
	}
	name := "cpa-prometheus_0.0.0-test_" + runtime.GOOS + "_" + runtime.GOARCH + ".zip"
	archive, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePlatformZip(archive, "cpa-prometheus", runtime.GOOS); err != nil {
		t.Fatal(err)
	}
	checksums, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ChecksumsMatch(checksums, name, archive); err != nil {
		t.Fatal(err)
	}
}
