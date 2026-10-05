// Package fetch locates, downloads and caches the native Needle inference
// engine. The engine is published on the Hugging Face Hub as per-platform
// packages whose payload is the shared library (libneedle.so, libneedle.dll
// or libneedle.dylib); this package downloads the package that matches the
// current machine, extracts the library, and caches it under
// ~/.cache/needle-go.
//
// Resolution order for an engine library:
//
//  1. the NEEDLE<GEN>_LIB_PATH environment variable (NEEDLE_LIB_PATH is a
//     legacy alias for generation 2 only, and is deliberately never used
//     for generation 3 archives);
//  2. a library placed next to the running executable;
//  3. the cache directory above;
//  4. a fresh download from the Hub, unless HF_HUB_OFFLINE=1.
package fetch

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ProgressHook, when set, receives download progress callbacks. Assign it
// before the first engine fetch if you want progress reporting.
var ProgressHook ProgressFunc

func client() *httpClient { return newClient(ProgressHook) }

// LibraryPath resolves the engine library path for a generation, fetching
// it from the Hugging Face Hub on first use. It is the entry point the
// agent uses to find the engine.
func LibraryPath(ctx context.Context, generation int) (string, error) {
	if _, err := EngineRepo(generation); err != nil {
		return "", err
	}
	// 1. environment override
	if override := envOverride(generation); override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("needle: %s points at %s which does not exist", envOverrideName(generation), override)
		}
		return override, nil
	}
	// 2. next to the executable
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, name := range localLibNames(generation) {
			local := filepath.Join(dir, name)
			if _, err := os.Stat(local); err == nil {
				return local, nil
			}
		}
	}
	// 3. the shared cache
	version, err := EngineVersion(generation)
	if err != nil {
		return "", err
	}
	cacheDir, err := CacheDirFor(generation, version)
	if err != nil {
		return "", err
	}
	cached := filepath.Join(cacheDir, localLibNames(generation)[0])
	if _, err := os.Stat(cached); err == nil {
		return cached, nil
	}
	// 4. download, unless offline was requested
	if Offline() {
		return "", fmt.Errorf("needle: engine library for generation %d not found and HF_HUB_OFFLINE=1 prevents downloading; "+
			"place %s in %s or set %s", generation, LibName(), cacheDir, envOverrideName(generation))
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	return FetchLibrary(ctx, generation, version, "", cacheDir)
}

// BaseWeightsPath resolves the published base weights archive for a
// generation, downloading it into the generation's engine cache when needed.
func BaseWeightsPath(ctx context.Context, generation int) (string, error) {
	repo, err := EngineRepo(generation)
	if err != nil {
		return "", err
	}
	name, ok := map[int]string{3: "needle3.cact"}[generation]
	if !ok {
		return "", fmt.Errorf("unsupported base weights for Needle generation: %d", generation)
	}
	version, err := EngineVersion(generation)
	if err != nil {
		return "", err
	}
	dir, err := CacheDirFor(generation, version)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if Offline() {
		return "", fmt.Errorf("needle: base weights for generation %d not found and HF_HUB_OFFLINE=1 prevents downloading; place %s in %s", generation, name, dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if _, err := client().downloadToFile(ctx, name, resolveURL(repo, name), path, false); err != nil {
		return "", err
	}
	return path, nil
}

func envOverrideName(generation int) string {
	return fmt.Sprintf("NEEDLE%d_LIB_PATH", generation)
}

func envOverride(generation int) string {
	if v := strings.TrimSpace(os.Getenv(fmt.Sprintf("NEEDLE%d_LIB_PATH", generation))); v != "" {
		return v
	}
	if generation == 2 {
		// NEEDLE_LIB_PATH predates multi-generation dispatch and therefore
		// names the Needle 2 engine. Never route a v3 archive through it.
		if v := strings.TrimSpace(os.Getenv("NEEDLE_LIB_PATH")); v != "" {
			return v
		}
	}
	return ""
}

// localLibNames lists the library file names accepted next to the
// executable. Wheels published before the multi-generation split shipped
// Needle 2 as plain libneedle.*, hence the legacy alias for generation 2.
func localLibNames(generation int) []string {
	lib := LibName()
	stem, suffix := strings.TrimSuffix(lib, filepath.Ext(lib)), filepath.Ext(lib)
	names := []string{fmt.Sprintf("%s%d%s", stem, generation, suffix)}
	if generation == 2 {
		names = append(names, lib)
	}
	return names
}

// FetchLibrary downloads the engine package for a platform tag (the
// current machine when empty), extracts the shared library into destDir
// and returns its path.
func FetchLibrary(ctx context.Context, generation int, version, tag, destDir string) (string, error) {
	repo, err := EngineRepo(generation)
	if err != nil {
		return "", err
	}
	if version == "" {
		if version, err = EngineVersion(generation); err != nil {
			return "", err
		}
	}
	if tag == "" {
		if tag, err = PlatformTag(); err != nil {
			return "", err
		}
	}
	if destDir == "" {
		return "", fmt.Errorf("needle: FetchLibrary needs a destination directory")
	}
	wheel := fmt.Sprintf("cactus_needle-%s-py3-none-%s.whl", version, tag)
	wheelURL := resolveURL(repo, "python/"+wheel)
	wheelPath := filepath.Join(os.TempDir(), "needle-"+version+"-"+tag+".whl")
	c := client()
	if _, err := c.downloadToFile(ctx, wheel, wheelURL, wheelPath, false); err != nil {
		return "", fmt.Errorf("needle: cannot fetch engine wheel %s: %w", wheel, err)
	}
	defer os.Remove(wheelPath)
	libName := libNameForGeneration(generation, tag)
	if tag == mustPlatformTag() {
		libName = localLibNames(generation)[0]
	}
	return extractWheelLibrary(wheelPath, libName, destDir)
}

func mustPlatformTag() string {
	tag, err := PlatformTag()
	if err != nil {
		return ""
	}
	return tag
}

// extractWheelLibrary pulls needle/<libName> out of a wheel (which is a
// plain zip archive) into destDir atomically.
func extractWheelLibrary(wheelPath, libName, destDir string) (string, error) {
	r, err := zip.OpenReader(wheelPath)
	if err != nil {
		return "", fmt.Errorf("needle: engine wheel %s is not a valid archive: %w", wheelPath, err)
	}
	defer r.Close()
	member := "needle/" + libName
	var found *zip.File
	for _, f := range r.File {
		if f.Name == member {
			found = f
			break
		}
	}
	if found == nil {
		return "", fmt.Errorf("needle: engine wheel %s does not contain %s", wheelPath, member)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(destDir, libName)
	part := out + ".part"
	f, err := os.Create(part)
	if err != nil {
		return "", err
	}
	src, err := found.Open()
	if err != nil {
		f.Close()
		os.Remove(part)
		return "", err
	}
	if _, err := io.Copy(f, src); err != nil {
		src.Close()
		f.Close()
		os.Remove(part)
		return "", err
	}
	src.Close()
	if err := f.Close(); err != nil {
		os.Remove(part)
		return "", err
	}
	_ = os.Chmod(part, 0o644)
	if err := os.Rename(part, out); err != nil {
		os.Remove(part)
		return "", err
	}
	return out, nil
}

// DownloadPlatform copies one platform's standalone engine-runner files
// (for example linux-x86_64 or wasm) into outDir/<platform>/ and marks the
// native runner executable where present. It returns the written paths.
func DownloadPlatform(ctx context.Context, generation int, platform, outDir string) ([]string, error) {
	repo, err := EngineRepo(generation)
	if err != nil {
		return nil, err
	}
	if !validPlatform(platform) {
		return nil, fmt.Errorf("unknown platform %q, pick one of: %s", platform, strings.Join(Platforms, ", "))
	}
	c := client()
	files, err := c.listRepoFiles(ctx, repo)
	if err != nil {
		return nil, err
	}
	dest := filepath.Join(outDir, platform)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		if !strings.HasPrefix(f, platform+"/") {
			continue
		}
		base := filepath.Base(f)
		executable := base == "needle" || base == "needle.exe"
		target := filepath.Join(dest, base)
		if _, err := c.downloadToFile(ctx, base, resolveURL(repo, f), target, executable && runtime.GOOS != "windows"); err != nil {
			return nil, err
		}
		out = append(out, target)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("platform %q holds no files in %s", platform, repo)
	}
	return out, nil
}

func validPlatform(name string) bool {
	for _, p := range Platforms {
		if p == name {
			return true
		}
	}
	return false
}

// DownloadWeights pulls a published .cact weights archive from the Hub.
// The spec is either "<org>/<repo>/<file>.cact" or "<org>/<repo>" when the
// repository holds exactly one archive. The file is placed in outDir and
// its path returned.
func DownloadWeights(ctx context.Context, spec, outDir string) (string, error) {
	parts := strings.Split(strings.Trim(spec, "/"), "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("pass <org>/<repo>/<file>.cact or <org>/<repo>")
	}
	repo := strings.Join(parts[:2], "/")
	filename := strings.Join(parts[2:], "/")
	c := client()
	if filename == "" {
		files, err := c.listRepoFiles(ctx, repo)
		if err != nil {
			return "", err
		}
		var cacts []string
		for _, f := range files {
			if strings.HasSuffix(f, ".cact") {
				cacts = append(cacts, f)
			}
		}
		if len(cacts) != 1 {
			shown := cacts
			if len(shown) > 5 {
				shown = shown[:5]
			}
			return "", fmt.Errorf("%s holds %d .cact files, name one: %s", repo, len(cacts), strings.Join(shown, ", "))
		}
		filename = cacts[0]
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(outDir, filepath.Base(filename))
	return c.downloadToFile(ctx, filepath.Base(filename), resolveURL(repo, filename), dest, false)
}
