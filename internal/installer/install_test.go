// Installer integration tests use fake downloads and never contact GitHub.
package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstaller(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, os, arch, version                   string
		corrupt, missingChecksum, downloadFailure bool
		want                                      string
	}{
		{name: "latest amd64", os: "Linux", arch: "x86_64"},
		{name: "pinned arm64", os: "Linux", arch: "aarch64", version: "1.2.3"},
		{name: "prefixed version", os: "Linux", arch: "amd64", version: "v1.2.3"},
		{name: "checksum mismatch", os: "Linux", arch: "x86_64", corrupt: true, want: "Checksum mismatch"},
		{name: "missing checksum", os: "Linux", arch: "x86_64", missingChecksum: true, want: "invalid release checksum"},
		{name: "download failure", os: "Linux", arch: "x86_64", downloadFailure: true, want: "Download failed"},
		{name: "unsupported OS", os: "Darwin", arch: "arm64", want: "Only Linux"},
		{name: "unsupported arch", os: "Linux", arch: "riscv64", want: "Supported architectures"},
		{name: "invalid version", os: "Linux", arch: "x86_64", version: "v1.2.3/../../other", want: "stable version"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tools := filepath.Join(root, "tools")
			fixtures := filepath.Join(root, "fixtures")
			installDir := filepath.Join(root, "install with spaces")
			for _, dir := range []string{tools, fixtures, installDir} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			payload := []byte("#!/bin/sh\nprintf 'aiu v1.2.3\\n'\n")
			var archive bytes.Buffer
			gz := gzip.NewWriter(&archive)
			tarWriter := tar.NewWriter(gz)
			if err := tarWriter.WriteHeader(&tar.Header{Name: "aiu", Mode: 0755, Size: int64(len(payload))}); err != nil {
				t.Fatal(err)
			}
			if _, err := tarWriter.Write(payload); err != nil {
				t.Fatal(err)
			}
			if err := tarWriter.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(archive.Bytes())
			var manifest strings.Builder
			for _, arch := range []string{"amd64", "arm64"} {
				asset := "aiu_linux_" + arch + ".tar.gz"
				if err := os.WriteFile(filepath.Join(fixtures, asset), archive.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
				if !tt.missingChecksum {
					fmt.Fprintf(&manifest, "%x  %s\n", sum, asset)
				}
				if tt.corrupt {
					if err := os.WriteFile(filepath.Join(fixtures, asset), []byte("corrupted download"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := os.WriteFile(filepath.Join(fixtures, "SHA256SUMS"), []byte(manifest.String()), 0600); err != nil {
				t.Fatal(err)
			}
			fakeCurl := `#!/bin/sh
set -eu
output=
while [ "$#" -gt 0 ]; do
    case "$1" in -o) output=$2; shift ;; https://*) url=$1 ;; esac
    shift
done
printf '%s\n' "$url" >> "$URL_LOG"
[ "$DOWNLOAD_FAILURE" = false ] || exit 22
cp "$FIXTURES/${url##*/}" "$output"
`
			fakeUname := `#!/bin/sh
case "$1" in -s) printf '%s\n' "$TEST_OS" ;; -m) printf '%s\n' "$TEST_ARCH" ;; esac
`
			for name, content := range map[string]string{"curl": fakeCurl, "uname": fakeUname} {
				if err := os.WriteFile(filepath.Join(tools, name), []byte(content), 0755); err != nil {
					t.Fatal(err)
				}
			}
			// Failed downloads must not replace an existing installation.
			binary := filepath.Join(installDir, "aiu")
			if err := os.WriteFile(binary, []byte("old installation"), 0755); err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(root, "urls")
			cmd := exec.Command("sh")
			cmd.Stdin = bytes.NewReader(script) // Exercise the same stdin path as curl | sh.
			cmd.Env = append(os.Environ(), "PATH="+tools+":"+os.Getenv("PATH"), "FIXTURES="+fixtures, "URL_LOG="+log, "TEST_OS="+tt.os, "TEST_ARCH="+tt.arch, "AIU_VERSION="+tt.version, "AIU_INSTALL_DIR="+installDir, "TMPDIR="+root, fmt.Sprintf("DOWNLOAD_FAILURE=%t", tt.downloadFailure))
			output, runErr := cmd.CombinedOutput()
			installed, err := os.ReadFile(binary)
			if err != nil {
				t.Fatal(err)
			}
			if tt.want != "" {
				if runErr == nil || !strings.Contains(string(output), tt.want) {
					t.Fatalf("expected %q failure: %v\n%s", tt.want, runErr, output)
				}
				if string(installed) != "old installation" {
					t.Fatal("failed install replaced existing binary")
				}
			} else {
				if runErr != nil {
					t.Fatalf("%v\n%s", runErr, output)
				}
				if !bytes.Equal(installed, payload) {
					t.Fatal("wrong binary installed")
				}
				info, err := os.Stat(binary)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0755 {
					t.Fatal("not executable")
				}
				urls, err := os.ReadFile(log)
				if err != nil {
					t.Fatal(err)
				}
				path := "/releases/latest/download/"
				if tt.version != "" {
					path = "/releases/download/v1.2.3/"
				}
				if !strings.Contains(string(urls), path) {
					t.Fatal(string(urls))
				}
				arch := "amd64"
				if tt.arch == "aarch64" {
					arch = "arm64"
				}
				if !strings.Contains(string(urls), "aiu_linux_"+arch+".tar.gz") {
					t.Fatal(string(urls))
				}
			}
			for _, pattern := range []string{filepath.Join(root, "aiu-download.*"), filepath.Join(installDir, ".aiu-install.*")} {
				matches, err := filepath.Glob(pattern)
				if err != nil {
					t.Fatal(err)
				}
				if len(matches) != 0 {
					t.Fatal("temporary files not cleaned up", matches)
				}
			}
		})
	}
}
