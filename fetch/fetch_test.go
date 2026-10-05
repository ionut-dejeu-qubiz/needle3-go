package fetch

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPlatformTagForMatrix(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
	}{
		{"darwin", "arm64", "macosx_11_0_arm64"},
		{"darwin", "amd64", "macosx_11_0_x86_64"},
		{"windows", "amd64", "win_amd64"},
		{"windows", "arm64", "win_arm64"},
	}
	for _, tc := range cases {
		got, err := PlatformTagFor(tc.goos, tc.goarch)
		if err != nil {
			t.Errorf("%s/%s: %v", tc.goos, tc.goarch, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s/%s = %q, want %q", tc.goos, tc.goarch, got, tc.want)
		}
	}
	// linux glibc tags (musl detection depends on the host, so only check
	// the suffix when the host is glibc linux)
	if runtime.GOOS == "linux" && !isMusl() {
		got, err := PlatformTagFor("linux", "amd64")
		if err != nil || got != "manylinux2014_x86_64" {
			t.Errorf("linux/amd64 = %q, %v", got, err)
		}
		got, err = PlatformTagFor("linux", "arm64")
		if err != nil || got != "manylinux2014_aarch64" {
			t.Errorf("linux/arm64 = %q, %v", got, err)
		}
	}
	// unsupported machine without wheels
	if _, err := PlatformTagFor("linux", "riscv64"); err == nil {
		t.Errorf("linux/riscv64 should have no wheel")
	}
	if _, err := PlatformTagFor("plan9", "amd64"); err == nil {
		t.Errorf("plan9/amd64 should have no wheel")
	}
}

func TestLibName(t *testing.T) {
	name := LibName()
	switch runtime.GOOS {
	case "windows":
		if name != "libneedle.dll" {
			t.Errorf("LibName = %q", name)
		}
	case "darwin":
		if name != "libneedle.dylib" {
			t.Errorf("LibName = %q", name)
		}
	default:
		if name != "libneedle.so" {
			t.Errorf("LibName = %q", name)
		}
	}
}

func TestEngineRepoVersion(t *testing.T) {
	if repo, _ := EngineRepo(2); repo != "Cactus-Compute/needle2" {
		t.Errorf("repo = %q", repo)
	}
	if v, _ := EngineVersion(2); v != "2.0.4" {
		t.Errorf("version = %q", v)
	}
	if _, err := EngineRepo(4); err == nil {
		t.Error("generation 4 should be unsupported")
	}
}

func TestLibNameForTag(t *testing.T) {
	if LibNameForTag("win_amd64") != "libneedle.dll" {
		t.Error("win tag should map to dll")
	}
	if LibNameForTag("macosx_11_0_arm64") != "libneedle.dylib" {
		t.Error("macos tag should map to dylib")
	}
	if LibNameForTag("manylinux2014_x86_64") != "libneedle.so" {
		t.Error("linux tag should map to so")
	}
	if got := libNameForGeneration(3, "win_amd64"); got != "libneedle3.dll" {
		t.Errorf("generation 3 Windows library = %q", got)
	}
	if got := libNameForGeneration(2, "win_amd64"); got != "libneedle.dll" {
		t.Errorf("generation 2 Windows library = %q", got)
	}
}

func TestValidPlatform(t *testing.T) {
	for _, p := range []string{"linux-x86_64", "windows-x86_64", "wasm", "wasm-component"} {
		if !validPlatform(p) {
			t.Errorf("%q should be valid", p)
		}
	}
	if validPlatform("linux-x86") {
		t.Error("linux-x86 should not be valid")
	}
}

func TestLibraryPathEnvOverride(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, LibName())
	if err := os.WriteFile(lib, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEEDLE2_LIB_PATH", lib)
	got, err := LibraryPath(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != lib {
		t.Errorf("LibraryPath = %q, want %q", got, lib)
	}
}

func TestLibraryPathLegacyEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	lib := filepath.Join(dir, LibName())
	if err := os.WriteFile(lib, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEEDLE_LIB_PATH", lib)
	got, err := LibraryPath(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != lib {
		t.Errorf("LibraryPath = %q, want %q (legacy alias)", got, lib)
	}
	// The legacy alias must never serve generation 3.
	if got := envOverride(3); got != "" {
		t.Errorf("generation 3 resolved through legacy variable: %q", got)
	}
}

func TestLibraryPathOfflineFailsFast(t *testing.T) {
	t.Setenv("NEEDLE2_LIB_PATH", "")
	t.Setenv("NEEDLE_LIB_PATH", "")
	t.Setenv("HF_HUB_OFFLINE", "1")
	t.Setenv("HOME", "/nonexistent-needle-home")
	t.Setenv("USERPROFILE", "/nonexistent-needle-home")
	// Point the executable lookup at a temp dir so a stray library is not
	// picked up next to the test binary.
	if _, err := LibraryPath(context.Background(), 3); err == nil {
		t.Error("offline mode without an engine should fail fast")
	}
}
