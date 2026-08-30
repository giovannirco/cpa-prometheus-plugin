package release

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
)

type Registry struct {
	SchemaVersion int      `json:"schema_version"`
	Plugins       []Plugin `json:"plugins"`
}

type Plugin struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Repository  string   `json:"repository"`
	Homepage    string   `json:"homepage"`
	License     string   `json:"license"`
	Tags        []string `json:"tags"`
}

func ValidateRegistry(raw []byte, wantRepo string) error {
	var reg Registry
	if err := json.Unmarshal(raw, &reg); err != nil {
		return fmt.Errorf("registry json: %w", err)
	}
	if reg.SchemaVersion != 1 {
		return fmt.Errorf("schema_version = %d, want 1", reg.SchemaVersion)
	}
	if len(reg.Plugins) != 1 {
		return fmt.Errorf("plugins len = %d, want 1", len(reg.Plugins))
	}
	p := reg.Plugins[0]
	for _, field := range []struct{ name, value string }{
		{"id", p.ID},
		{"name", p.Name},
		{"description", p.Description},
		{"author", p.Author},
		{"repository", p.Repository},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("missing %s", field.name)
		}
	}
	if p.ID != "cpa-prometheus" {
		return fmt.Errorf("id = %q, want cpa-prometheus", p.ID)
	}
	parsed, err := url.Parse(p.Repository)
	if err != nil {
		return fmt.Errorf("repository url: %w", err)
	}
	if parsed.Host != "github.com" || parsed.Path != "/giovannirco/cpa-prometheus-plugin" {
		return fmt.Errorf("repository host/path = %s%s, want github.com/giovannirco/cpa-prometheus-plugin", parsed.Host, parsed.Path)
	}
	if wantRepo != "" && p.Repository != wantRepo {
		return fmt.Errorf("repository = %q, want %q", p.Repository, wantRepo)
	}
	return nil
}

func ValidateLinuxZip(data []byte, pluginID string) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	want := pluginID + ".so"
	var found string
	for _, file := range reader.File {
		name := strings.TrimPrefix(file.Name, "./")
		if strings.HasSuffix(name, "/") {
			continue
		}
		if strings.Contains(name, "/") {
			return fmt.Errorf("nested path not allowed: %s", file.Name)
		}
		if strings.HasSuffix(name, ".so") || strings.HasSuffix(name, ".dylib") || strings.HasSuffix(name, ".dll") {
			if found != "" {
				return fmt.Errorf("multiple dynamic libraries")
			}
			found = name
		}
	}
	if found != want {
		return fmt.Errorf("zip root library = %q, want %q", found, want)
	}
	return nil
}

func ChecksumsMatch(checksums []byte, filename string, zipData []byte) error {
	sum := sha256.Sum256(zipData)
	want := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(checksums), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		if parts[len(parts)-1] == filename || strings.HasSuffix(parts[len(parts)-1], filename) {
			if !strings.EqualFold(parts[0], want) {
				return fmt.Errorf("checksum %s != %s", parts[0], want)
			}
			return nil
		}
	}
	return fmt.Errorf("filename %s not in checksums.txt", filename)
}

func ZipRootLibrary(pluginID, goos string, library []byte) ([]byte, error) {
	ext := ".so"
	switch goos {
	case "darwin":
		ext = ".dylib"
	case "windows":
		ext = ".dll"
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(pluginID + ext)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(w, bytes.NewReader(library)); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
