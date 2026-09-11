package fetch

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// EngineRepos maps an engine generation to the Hugging Face repository that
// publishes its binaries.
var EngineRepos = map[int]string{
	2: "Cactus-Compute/needle2",
	3: "Cactus-Compute/needle3",
}

// EngineVersions maps an engine generation to the published engine build
// this package downloads.
var EngineVersions = map[int]string{
	2: "2.0.4",
	3: "3.0.0",
}

// Platforms is the list of standalone engine-runner platform folders in the
// Hugging Face repositories, used by DownloadPlatform.
var Platforms = []string{
	"macos-arm64", "linux-x86_64", "linux-arm64", "linux-armv7",
	"linux-riscv64", "linux-mipsel", "windows-x86_64", "windows-arm64",
	"android-arm64", "android-armv7", "android-riscv64",
	"ios-arm64", "ios-sim-arm64", "tvos-arm64", "watchos-arm64", "wasm",
	"wasm-component",
}

// EngineRepo returns the Hugging Face repository of an engine generation.
func EngineRepo(generation int) (string, error) {
	repo, ok := EngineRepos[generation]
	if !ok {
		return "", fmt.Errorf("unsupported Needle generation: %d", generation)
	}
	return repo, nil
}

// EngineVersion returns the engine build version of a generation.
func EngineVersion(generation int) (string, error) {
	version, ok := EngineVersions[generation]
	if !ok {
		return "", fmt.Errorf("unsupported Needle generation: %d", generation)
	}
	return version, nil
}

// LibName returns the file name of the engine shared library for the
// current operating system.
func LibName() string {
	switch runtime.GOOS {
	case "darwin":
		return "libneedle.dylib"
	case "windows":
		return "libneedle.dll"
	default:
		return "libneedle.so"
	}
}

// LibNameForTag returns the library file name inside a wheel built for the
// given platform tag.
func LibNameForTag(tag string) string {
	switch {
	case strings.HasPrefix(tag, "macosx"):
		return "libneedle.dylib"
	case strings.HasPrefix(tag, "win"):
		return "libneedle.dll"
	default:
		return "libneedle.so"
	}
}

// PlatformTag returns the wheel platform tag matching the current machine,
// e.g. manylinux2014_x86_64, musllinux_1_2_aarch64, win_amd64 or
// macosx_11_0_arm64. An error is returned for machines without a published
// wheel build; those can still use an engine placed manually (see
// LibraryPath).
func PlatformTag() (string, error) {
	return PlatformTagFor(runtime.GOOS, runtime.GOARCH)
}

// PlatformTagFor maps an explicit GOOS/GOARCH pair to its wheel tag,
// detecting the libc flavour on Linux.
func PlatformTagFor(goos, goarch string) (string, error) {
	switch goos {
	case "darwin":
		if goarch == "arm64" {
			return "macosx_11_0_arm64", nil
		}
		return "macosx_11_0_x86_64", nil
	case "windows":
		switch goarch {
		case "arm64":
			return "win_arm64", nil
		case "amd64":
			return "win_amd64", nil
		}
		return "", fmt.Errorf("no Needle engine wheel for windows/%s", goarch)
	case "linux":
		arch := ""
		switch goarch {
		case "arm64", "aarch64":
			arch = "aarch64"
		case "amd64", "x86_64":
			arch = "x86_64"
		default:
			return "", fmt.Errorf("no Needle engine wheel for linux/%s; "+
				"build the engine from the platform's libneedle.a or set NEEDLE_LIB_PATH to a compatible library", goarch)
		}
		if isMusl() {
			return "musllinux_1_2_" + arch, nil
		}
		return "manylinux2014_" + arch, nil
	}
	return "", fmt.Errorf("no Needle engine wheel for %s/%s", goos, goarch)
}

// isMusl reports whether the current Linux userland is musl-based (Alpine
// and friends), which needs the musllinux engine build.
func isMusl() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if data, err := os.ReadFile("/proc/self/maps"); err == nil {
		if strings.Contains(string(data), "musl") {
			return true
		}
	}
	for _, pattern := range []string{"/lib/ld-musl-*.so*", "/usr/lib/ld-musl-*.so*"} {
		if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
			return true
		}
	}
	return false
}

// CacheDir returns the per-generation engine cache directory
// ~/.cache/needle-go/v<gen>/<version>.
func CacheDir(generation int) (string, error) {
	version, err := EngineVersion(generation)
	if err != nil {
		return "", err
	}
	return CacheDirFor(generation, version)
}

// CacheDirFor is CacheDir with an explicit engine version.
func CacheDirFor(generation int, version string) (string, error) {
	if _, err := EngineRepo(generation); err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("needle: cannot determine home directory for the engine cache: %w", err)
	}
	return filepath.Join(home, ".cache", "needle-go",
		fmt.Sprintf("v%d", generation), version), nil
}
