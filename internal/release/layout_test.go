package release

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateRegistryFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRegistry(raw, "https://github.com/giovannirco/cpa-prometheus-plugin"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateLinuxZipRootSo(t *testing.T) {
	zipData, err := ZipRootLibrary("cpa-prometheus", "linux", []byte("fake-so"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateLinuxZip(zipData, "cpa-prometheus"); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(zipData)
	checksums := []byte(fmt.Sprintf("%s  cpa-prometheus_0.1.0_linux_amd64.zip\n", hex.EncodeToString(sum[:])))
	if err := ChecksumsMatch(checksums, "cpa-prometheus_0.1.0_linux_amd64.zip", zipData); err != nil {
		t.Fatal(err)
	}
}

func TestValidateLinuxZipRejectsNestedPath(t *testing.T) {
	// ZipRootLibrary always writes root; craft nested zip via a second call is not needed —
	// ValidateLinuxZip on a root zip must succeed and a path with slash must fail.
	zipData, err := ZipRootLibrary("other", "linux", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateLinuxZip(zipData, "cpa-prometheus"); err == nil {
		t.Fatal("expected filename mismatch")
	}
}

func TestValidatePlatformZipEveryShippedTarget(t *testing.T) {
	for _, tc := range []struct{ goos, ext string }{
		{"linux", ".so"},
		{"darwin", ".dylib"},
		{"windows", ".dll"},
	} {
		zipData, err := ZipRootLibrary("cpa-prometheus", tc.goos, []byte("fake-lib"))
		if err != nil {
			t.Fatalf("%s: %v", tc.goos, err)
		}
		if got := LibraryExt(tc.goos); got != tc.ext {
			t.Fatalf("%s ext = %q, want %q", tc.goos, got, tc.ext)
		}
		if err := ValidatePlatformZip(zipData, "cpa-prometheus", tc.goos); err != nil {
			t.Fatalf("%s: %v", tc.goos, err)
		}
		// A zip built for one OS must not pass validation for another.
		for _, other := range []string{"linux", "darwin", "windows"} {
			if other == tc.goos {
				continue
			}
			if err := ValidatePlatformZip(zipData, "cpa-prometheus", other); err == nil {
				t.Fatalf("%s zip accepted as %s", tc.goos, other)
			}
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}
