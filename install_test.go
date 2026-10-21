package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// toolDir returns a PATH dir with only the tools that install.sh uses, so the
// script finds no Go to fall back to. CI runners have Go in /usr/bin.
func toolDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, tool := range []string{"awk", "chmod", "curl", "cut", "dirname", "head", "mkdir", "mktemp", "mv", "rm", "sed", "sha256sum", "shasum", "uname"} {
		if p, err := exec.LookPath(tool); err == nil {
			if err := os.Symlink(p, filepath.Join(dir, tool)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

// Runs scripts/install.sh against a fake release directory.
func TestInstallScript(t *testing.T) {
	asset := fmt.Sprintf("herdr-cmd-status-%s-%s", runtime.GOOS, runtime.GOARCH)
	binary := []byte("#!/bin/sh\necho fake\n")
	good := fmt.Sprintf("%x  %s\n", sha256.Sum256(binary), asset)
	bad := fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte("other")), asset)

	tests := []struct {
		name    string
		sums    string
		wantErr string
	}{
		{"matching checksum installs the binary", good, ""},
		{"wrong checksum is rejected", bad, "checksum"},
		{"missing release falls back and fails without Go", "", "no Go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			release := filepath.Join(t.TempDir(), "v0.1.0")
			write(t, filepath.Join(root, "herdr-plugin.toml"), "version = \"0.1.0\"\n")
			script, err := os.ReadFile("scripts/install.sh")
			if err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(root, "scripts", "install.sh"), string(script))
			if tt.sums != "" {
				write(t, filepath.Join(release, asset), string(binary))
				write(t, filepath.Join(release, "SHA256SUMS"), tt.sums)
			}

			cmd := exec.Command("sh", filepath.Join(root, "scripts", "install.sh"))
			cmd.Env = []string{
				"PATH=" + toolDir(t),
				"HERDR_CMD_STATUS_BASE_URL=file://" + filepath.Dir(release),
			}
			out, err := cmd.CombinedOutput()
			installed, readErr := os.ReadFile(filepath.Join(root, "bin", "herdr-cmd-status"))

			if tt.wantErr == "" {
				if err != nil || string(installed) != string(binary) {
					t.Fatalf("install failed: %v\n%s", err, out)
				}
				return
			}
			if err == nil || !strings.Contains(string(out), tt.wantErr) {
				t.Fatalf("want failure with %q, got %v\n%s", tt.wantErr, err, out)
			}
			if readErr == nil {
				t.Fatalf("a rejected install left bin/herdr-cmd-status behind")
			}
		})
	}
}

func write(t *testing.T, file, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
