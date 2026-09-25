package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Versions
// ---------------------------------------------------------------------------

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"equal", "1.0.0", "1.0.0", 0},
		{"equal with v prefix", "v1.0.0", "1.0.0", 0},
		{"equal both v prefix", "v2.1.3", "v2.1.3", 0},
		{"a less major", "1.0.0", "2.0.0", -1},
		{"a less minor", "1.1.0", "1.2.0", -1},
		{"a less patch", "1.0.1", "1.0.2", -1},
		{"a greater major", "3.0.0", "2.9.9", 1},
		{"a greater minor", "1.2.0", "1.1.9", 1},
		{"a greater patch", "1.0.5", "1.0.4", 1},
		{"v prefix mixed", "v3.0.1", "3.0.0", 1},
		{"pre-release stripped", "2.0.0-pre", "1.9.9", 1},
		{"git describe stripped", "v1.2.0-4-gdeadbee", "1.2.0", 0},
		{"invalid a treated as 0.0.0", "invalid", "0.0.1", -1},
		{"invalid b treated as 0.0.0", "0.0.1", "invalid", 1},
		{"both invalid", "bad", "worse", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CompareVersions(tt.a, tt.b); got != tt.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestStripV(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"v1.0.0", "1.0.0"},
		{"1.0.0", "1.0.0"},
		{"v", ""},
		{"", ""},
		{"vv1.0", "v1.0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := stripV(tt.input); got != tt.want {
				t.Errorf("stripV(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name                      string
		input                     string
		wantMaj, wantMin, wantPat int
		wantErr                   bool
	}{
		{"simple", "1.2.3", 1, 2, 3, false},
		{"with v", "v3.0.1", 3, 0, 1, false},
		{"pre-release", "2.0.0-pre", 2, 0, 0, false},
		{"git describe", "v0.2.0-3-gdeadbee-dirty", 0, 2, 0, false},
		{"too few parts", "1.2", 0, 0, 0, true},
		{"four parts", "1.2.3.4", 0, 0, 0, true},
		{"non-numeric major", "a.2.3", 0, 0, 0, true},
		{"non-numeric minor", "1.b.3", 0, 0, 0, true},
		{"non-numeric patch", "1.2.c", 0, 0, 0, true},
		{"tag word", "latest", 0, 0, 0, true},
		{"empty", "", 0, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maj, min, pat, err := parseVersion(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}

			if err == nil && (maj != tt.wantMaj || min != tt.wantMin || pat != tt.wantPat) {
				t.Errorf("parseVersion(%q) = (%d, %d, %d), want (%d, %d, %d)",
					tt.input, maj, min, pat, tt.wantMaj, tt.wantMin, tt.wantPat)
			}
		})
	}
}

// A build that cannot say what it is must never be compared against a
// release: "devel" is what a plain `go build` of this repo reports.
func TestIsDevBuild(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", true},
		{"devel", true},
		{"latest", true},
		{"0.1.0", false},
		{"v0.1.0", false},
		{"v0.1.0-2-gabcdef", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsDevBuild(tt.input); got != tt.want {
				t.Errorf("IsDevBuild(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Assets
// ---------------------------------------------------------------------------

// The names have to match .goreleaser.yml exactly - a mismatch is an
// update that reports "no release asset found" for every platform.
func TestBuildAssetName(t *testing.T) {
	tests := []struct {
		name, version, goos, goarch, want string
	}{
		{"linux amd64", "1.2.3", "linux", "amd64", "ihttp_v1.2.3_linux_amd64.tar.gz"},
		{"linux arm64", "1.2.3", "linux", "arm64", "ihttp_v1.2.3_linux_arm64.tar.gz"},
		{"darwin amd64", "2.0.0", "darwin", "amd64", "ihttp_v2.0.0_darwin_amd64.tar.gz"},
		{"darwin arm64", "2.0.0", "darwin", "arm64", "ihttp_v2.0.0_darwin_arm64.tar.gz"},
		{"windows amd64", "1.2.3", "windows", "amd64", "ihttp_v1.2.3_windows_amd64.zip"},
		{"windows arm64", "1.0.0", "windows", "arm64", "ihttp_v1.0.0_windows_arm64.zip"},
		{"v prefix not doubled", "v1.0.0", "linux", "amd64", "ihttp_v1.0.0_linux_amd64.tar.gz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildAssetName(tt.version, tt.goos, tt.goarch)
			if got != tt.want {
				t.Errorf("BuildAssetName(%q, %q, %q) = %q, want %q",
					tt.version, tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}

func TestFindAssetURL(t *testing.T) {
	assets := []Asset{
		{Name: "ihttp_v1.2.3_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux_amd64.tar.gz"},
		{Name: "ihttp_v1.2.3_darwin_arm64.tar.gz", BrowserDownloadURL: "https://example.com/darwin_arm64.tar.gz"},
		{Name: "checksums.sha256", BrowserDownloadURL: "https://example.com/checksums.sha256"},
	}

	tests := []struct {
		name, search, want string
	}{
		{"found linux", "ihttp_v1.2.3_linux_amd64.tar.gz", "https://example.com/linux_amd64.tar.gz"},
		{"found checksums", "checksums.sha256", "https://example.com/checksums.sha256"},
		{"not found", "ihttp_v1.2.3_windows_amd64.zip", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findAssetURL(assets, tt.search); got != tt.want {
				t.Errorf("findAssetURL(_, %q) = %q, want %q", tt.search, got, tt.want)
			}
		})
	}

	t.Run("empty list", func(t *testing.T) {
		if got := findAssetURL(nil, "anything"); got != "" {
			t.Errorf("findAssetURL(nil, _) = %q, want empty", got)
		}
	})
}

// ---------------------------------------------------------------------------
// CheckLatestVersion
// ---------------------------------------------------------------------------

func TestCheckLatestVersionNewerAvailable(t *testing.T) {
	srv := releaseServer(t, Release{TagName: "v2.0.0"})
	useTestServer(t, srv)

	res := CheckLatestVersion(t.Context(), "1.2.3")
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}

	if !res.Available() || res.LatestVersion != "2.0.0" {
		t.Errorf("LatestVersion = %q, want %q", res.LatestVersion, "2.0.0")
	}
}

func TestCheckLatestVersionUpToDate(t *testing.T) {
	srv := releaseServer(t, Release{TagName: "v1.2.3"})
	useTestServer(t, srv)

	res := CheckLatestVersion(t.Context(), "1.2.3")
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}

	if res.Available() {
		t.Errorf("LatestVersion = %q, want empty (up to date)", res.LatestVersion)
	}
}

// A dev build asks GitHub nothing at all: the server here fails the test
// if it is reached.
func TestCheckLatestVersionDevBuild(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("a dev build must not call the release API")
	}))
	t.Cleanup(srv.Close)
	useTestServer(t, srv)

	for _, version := range []string{"", "devel"} {
		res := CheckLatestVersion(t.Context(), version)
		if res.Err != nil || res.Available() {
			t.Errorf("CheckLatestVersion(%q) = %+v, want an empty result", version, res)
		}
	}
}

func TestCheckLatestVersionAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	useTestServer(t, srv)

	if res := CheckLatestVersion(t.Context(), "1.2.3"); res.Err == nil {
		t.Fatal("expected an error for a 500 response, got nil")
	}
}

// A tag that is not a version is reported, not silently taken as 0.0.0
// and offered as an "update" to an older build.
func TestCheckLatestVersionUnparsableTag(t *testing.T) {
	srv := releaseServer(t, Release{TagName: "nightly"})
	useTestServer(t, srv)

	res := CheckLatestVersion(t.Context(), "1.2.3")
	if res.Err == nil {
		t.Fatal("expected an error for an unparsable tag, got nil")
	}

	if res.Available() {
		t.Errorf("LatestVersion = %q, want empty", res.LatestVersion)
	}
}

// ---------------------------------------------------------------------------
// Checksums
// ---------------------------------------------------------------------------

func TestVerifyChecksum(t *testing.T) {
	const assetName = "ihttp_v1.2.3_linux_amd64.tar.gz"

	content := []byte("test binary content for checksum verification")
	archivePath := writeTempFile(t, content)

	h := sha256.Sum256(content)
	correctHash := hex.EncodeToString(h[:])

	checksums := fmt.Sprintf(
		"%s  %s\nabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890  other_file.tar.gz\n",
		correctHash, assetName,
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, checksums)
	}))
	t.Cleanup(srv.Close)

	assets := []Asset{{Name: "checksums.sha256", BrowserDownloadURL: srv.URL}}

	t.Run("valid checksum", func(t *testing.T) {
		if err := verifyChecksum(t.Context(), assets, archivePath, assetName); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("wrong asset name", func(t *testing.T) {
		if err := verifyChecksum(t.Context(), assets, archivePath, "nonexistent.tar.gz"); err == nil {
			t.Fatal("expected an error for a missing checksum entry, got nil")
		}
	})

	t.Run("tampered file", func(t *testing.T) {
		tampered := writeTempFile(t, []byte("tampered content"))

		err := verifyChecksum(t.Context(), assets, tampered, assetName)
		if err == nil {
			t.Fatal("expected a checksum mismatch error, got nil")
		}

		if !strings.Contains(err.Error(), "checksum mismatch") {
			t.Errorf("error = %q, want it to contain 'checksum mismatch'", err.Error())
		}
	})

	t.Run("missing checksums asset", func(t *testing.T) {
		if err := verifyChecksum(t.Context(), nil, archivePath, assetName); err == nil {
			t.Fatal("expected an error for a missing checksums.sha256 asset, got nil")
		}
	})
}

func TestParseChecksumFile(t *testing.T) {
	const hash = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"

	tests := []struct {
		name    string
		content string
		target  string
		wantErr bool
	}{
		{"plain", hash + "  file.tar.gz", "file.tar.gz", false},
		{"binary marker", hash + " *file.tar.gz", "file.tar.gz", false},
		{"path prefix", hash + "  dist/file.tar.gz", "file.tar.gz", false},
		{"among others", "0000  other.zip\n" + hash + "  file.tar.gz\n", "file.tar.gz", false},
		{"missing", hash + "  other.tar.gz", "file.tar.gz", true},
		{"empty", "", "file.tar.gz", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseChecksumFile(strings.NewReader(tt.content), tt.target)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}

			if err == nil && got != hash {
				t.Errorf("hash = %q, want %q", got, hash)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Archives
// ---------------------------------------------------------------------------

func TestExtractTarGz(t *testing.T) {
	binary := []byte("#!/bin/sh\necho hello\n")
	archivePath := writeTempFile(t, tarGzBytes(t, "ihttp", binary))

	t.Run("extract existing binary", func(t *testing.T) {
		data, err := extractTarGz(archivePath, "ihttp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !bytes.Equal(data, binary) {
			t.Errorf("extracted content = %q, want %q", data, binary)
		}
	})

	t.Run("directory prefix", func(t *testing.T) {
		nested := writeTempFile(t, tarGzBytes(t, "ihttp_v1.2.3_linux_amd64/ihttp", binary))

		data, err := extractTarGz(nested, "ihttp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !bytes.Equal(data, binary) {
			t.Errorf("extracted content = %q, want %q", data, binary)
		}
	})

	t.Run("binary not found", func(t *testing.T) {
		if _, err := extractTarGz(archivePath, "nonexistent"); err == nil {
			t.Fatal("expected an error for a missing binary, got nil")
		}
	})
}

func TestExtractZip(t *testing.T) {
	binary := []byte("MZ fake windows binary")
	archivePath := writeTempFile(t, zipBytes(t, "ihttp.exe", binary))

	t.Run("extract existing binary", func(t *testing.T) {
		data, err := extractZip(archivePath, "ihttp.exe")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !bytes.Equal(data, binary) {
			t.Errorf("extracted content = %q, want %q", data, binary)
		}
	})

	t.Run("binary not found", func(t *testing.T) {
		if _, err := extractZip(archivePath, "nonexistent"); err == nil {
			t.Fatal("expected an error for a missing binary, got nil")
		}
	})
}

// A directory entry named like the binary must not be extracted as a
// zero-byte binary, which would replace a working install with nothing.
func TestExtractZipSkipsDirectoryEntry(t *testing.T) {
	binary := []byte("#!/bin/sh\necho real binary\n")

	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)

	dirHdr := &zip.FileHeader{Name: "ihttp/"}
	dirHdr.SetMode(os.ModeDir | 0o755)

	if _, err := zw.CreateHeader(dirHdr); err != nil {
		t.Fatal(err)
	}

	fw, err := zw.Create("ihttp/ihttp")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := fw.Write(binary); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := extractZip(writeTempFile(t, buf.Bytes()), "ihttp")
	if err != nil {
		t.Fatalf("extractZip failed: %v", err)
	}

	if !bytes.Equal(data, binary) {
		t.Errorf("extracted content = %q, want %q", data, binary)
	}
}

func TestExtractZipOnlyDirectoryEntry(t *testing.T) {
	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)

	dirHdr := &zip.FileHeader{Name: "ihttp/"}
	dirHdr.SetMode(os.ModeDir | 0o755)

	if _, err := zw.CreateHeader(dirHdr); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := extractZip(writeTempFile(t, buf.Bytes()), "ihttp"); err == nil {
		t.Fatal("expected a 'not found' error for a directory-only archive, got nil")
	}
}

// The downloaded file is a temporary one with no meaningful extension,
// so the archive kind comes from the asset name - not from the path.
func TestExtractBinaryFormatFromAsset(t *testing.T) {
	binary := []byte("binary")

	t.Run("zip", func(t *testing.T) {
		path := writeTempFile(t, zipBytes(t, "ihttp.exe", binary))

		if _, err := extractBinary(path, "ihttp.exe", true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("tar.gz", func(t *testing.T) {
		path := writeTempFile(t, tarGzBytes(t, "ihttp", binary))

		if _, err := extractBinary(path, "ihttp", false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Replacing the binary
// ---------------------------------------------------------------------------

func TestAtomicReplace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a running executable cannot be renamed on Windows")
	}

	exePath := filepath.Join(t.TempDir(), "ihttp")
	if err := os.WriteFile(exePath, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("failed to write the test binary: %v", err)
	}

	newContent := []byte("new binary v2")
	if err := atomicReplace(exePath, newContent); err != nil {
		t.Fatalf("atomicReplace() error: %v", err)
	}

	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("failed to read the replaced binary: %v", err)
	}

	if !bytes.Equal(got, newContent) {
		t.Errorf("replaced content = %q, want %q", got, newContent)
	}

	if _, err := os.Stat(exePath + ".old"); !os.IsNotExist(err) {
		t.Errorf(".old file still exists: %v", err)
	}

	info, err := os.Stat(exePath)
	if err != nil {
		t.Fatalf("failed to stat the replaced binary: %v", err)
	}

	if info.Mode().Perm() != 0o755 {
		t.Errorf("permissions = %o, want 0755", info.Mode().Perm())
	}
}

// The whole path end to end: release lookup, download, checksum, extract
// and swap.
func TestDownloadAndReplace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a running executable cannot be renamed on Windows")
	}

	newBinary := []byte("#!/bin/sh\necho updated ihttp\n")
	assetName := BuildAssetName("2.0.0", runtime.GOOS, runtime.GOARCH)

	binName := "ihttp"
	archive := tarGzBytes(t, binName, newBinary)

	if strings.HasSuffix(assetName, ".zip") {
		binName += ".exe"
		archive = zipBytes(t, binName, newBinary)
	}

	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), assetName)

	mux := http.NewServeMux()
	srv := httptest.NewUnstartedServer(mux)

	mux.HandleFunc("/download/archive", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/download/checksums", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, checksums)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Release{
			TagName: "v2.0.0",
			Assets: []Asset{
				{Name: assetName, BrowserDownloadURL: srv.URL + "/download/archive"},
				{Name: "checksums.sha256", BrowserDownloadURL: srv.URL + "/download/checksums"},
			},
		})
	})

	srv.Start()
	t.Cleanup(srv.Close)
	useTestServer(t, srv)

	exePath := filepath.Join(t.TempDir(), binName)

	execPathFunc = func() (string, error) { return exePath, nil }
	t.Cleanup(func() { execPathFunc = nil })

	writeExe := func() {
		if err := os.WriteFile(exePath, []byte("old binary"), 0o755); err != nil {
			t.Fatalf("failed to write the test binary: %v", err)
		}
	}

	t.Run("already up to date", func(t *testing.T) {
		writeExe()

		var buf bytes.Buffer
		if err := DownloadAndReplace(t.Context(), "2.0.0", &buf); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(buf.String(), "Already up to date") {
			t.Errorf("output = %q, want it to contain 'Already up to date'", buf.String())
		}

		got, err := os.ReadFile(exePath)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got, []byte("old binary")) {
			t.Error("the binary was replaced although it was up to date")
		}
	})

	t.Run("update available", func(t *testing.T) {
		writeExe()

		var buf bytes.Buffer
		if err := DownloadAndReplace(t.Context(), "1.2.3", &buf); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(buf.String(), "Updated successfully") {
			t.Errorf("output = %q, want it to contain 'Updated successfully'", buf.String())
		}

		got, err := os.ReadFile(exePath)
		if err != nil {
			t.Fatalf("failed to read the replaced binary: %v", err)
		}

		if !bytes.Equal(got, newBinary) {
			t.Errorf("binary content = %q, want %q", got, newBinary)
		}
	})

	// A dev build has nothing to compare against, so it takes whatever
	// the latest release is.
	t.Run("dev build", func(t *testing.T) {
		writeExe()

		var buf bytes.Buffer
		if err := DownloadAndReplace(t.Context(), "devel", &buf); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		out := buf.String()
		if !strings.Contains(out, "development build") || !strings.Contains(out, "Updated to v2.0.0") {
			t.Errorf("output = %q, want it to report a development build and the update", out)
		}
	})

	// A release with no asset for this platform fails before anything is
	// written.
	t.Run("no asset for this platform", func(t *testing.T) {
		writeExe()

		bare := releaseServer(t, Release{TagName: "v3.0.0"})
		useTestServer(t, bare)

		var buf bytes.Buffer

		err := DownloadAndReplace(t.Context(), "1.2.3", &buf)
		if err == nil {
			t.Fatal("expected an error for a release with no matching asset, got nil")
		}

		if !strings.Contains(err.Error(), "no release asset found") {
			t.Errorf("error = %q, want it to contain 'no release asset found'", err.Error())
		}

		got, err := os.ReadFile(exePath)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got, []byte("old binary")) {
			t.Error("the binary was replaced although the update failed")
		}
	})
}

// A download whose bytes do not match the release checksums is never
// written over the binary.
func TestDownloadAndReplaceChecksumMismatch(t *testing.T) {
	assetName := BuildAssetName("2.0.0", runtime.GOOS, runtime.GOARCH)

	mux := http.NewServeMux()
	srv := httptest.NewUnstartedServer(mux)

	mux.HandleFunc("/download/archive", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not the archive that was signed"))
	})
	mux.HandleFunc("/download/checksums", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%064d  %s\n", 0, assetName)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Release{
			TagName: "v2.0.0",
			Assets: []Asset{
				{Name: assetName, BrowserDownloadURL: srv.URL + "/download/archive"},
				{Name: "checksums.sha256", BrowserDownloadURL: srv.URL + "/download/checksums"},
			},
		})
	})

	srv.Start()
	t.Cleanup(srv.Close)
	useTestServer(t, srv)

	exePath := filepath.Join(t.TempDir(), "ihttp")
	if err := os.WriteFile(exePath, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	execPathFunc = func() (string, error) { return exePath, nil }
	t.Cleanup(func() { execPathFunc = nil })

	var buf bytes.Buffer

	err := DownloadAndReplace(t.Context(), "1.2.3", &buf)
	if err == nil {
		t.Fatal("expected a checksum error, got nil")
	}

	if !strings.Contains(err.Error(), "checksum verification failed") {
		t.Errorf("error = %q, want it to contain 'checksum verification failed'", err.Error())
	}

	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("old binary")) {
		t.Error("the binary was replaced although the checksum did not match")
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// useTestServer points the package's HTTP client at srv, so the hardcoded
// GitHub API URL reaches the test server instead. Everything else - the
// asset URLs the release itself carries - is already srv's own.
func useTestServer(t *testing.T, srv *httptest.Server) {
	t.Helper()

	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	prev := httpClient

	SetHTTPClient(&http.Client{Transport: rewriteHost{base: base}})
	t.Cleanup(func() { SetHTTPClient(prev) })
}

// rewriteHost sends every request to base's host, keeping the path.
type rewriteHost struct{ base *url.URL }

func (t rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = t.base.Scheme
	req.URL.Host = t.base.Host
	req.Host = ""

	return http.DefaultTransport.RoundTrip(req)
}

// releaseServer answers every request with rel.
func releaseServer(t *testing.T, rel Release) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(rel)
	}))
	t.Cleanup(srv.Close)

	return srv
}

func writeTempFile(t *testing.T, content []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "archive")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("failed to write the temp file: %v", err)
	}

	return path
}

func tarGzBytes(t *testing.T, name string, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("failed to write the tar header: %v", err)
	}

	if _, err := tw.Write(content); err != nil {
		t.Fatalf("failed to write the tar content: %v", err)
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func zipBytes(t *testing.T, name string, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)

	fw, err := zw.Create(name)
	if err != nil {
		t.Fatalf("failed to create the zip entry: %v", err)
	}

	if _, err := fw.Write(content); err != nil {
		t.Fatalf("failed to write the zip content: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}
