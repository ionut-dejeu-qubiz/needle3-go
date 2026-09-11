package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultEndpoint is the Hugging Face Hub endpoint used for downloads.
// The HF_ENDPOINT environment variable overrides it, following the same
// convention as the standard Hub clients (useful for mirrors such as
// hf-mirror.com).
const DefaultEndpoint = "https://huggingface.co"

// UserAgent identifies this package to the Hub.
var UserAgent = "needle-go"

// Endpoint returns the active Hugging Face endpoint.
func Endpoint() string {
	if v := strings.TrimSpace(os.Getenv("HF_ENDPOINT")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return DefaultEndpoint
}

// Offline reports whether hub downloads are disabled via HF_HUB_OFFLINE.
func Offline() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("HF_HUB_OFFLINE")), "1") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("HF_HUB_OFFLINE")), "true")
}

// ProgressFunc receives download progress. total is -1 when the server did
// not report a content length.
type ProgressFunc func(label string, total, done int64)

type httpClient struct {
	client *http.Client
	onProg ProgressFunc
}

func newClient(onProg ProgressFunc) *httpClient {
	return &httpClient{
		// DefaultTransport already honors HTTP(S)_PROXY environment
		// variables, which is how huggingface_hub behaves too.
		client: &http.Client{Timeout: 0},
		onProg: onProg,
	}
}

func (c *httpClient) get(ctx context.Context, url string) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UserAgent)
		resp, err := c.client.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		// honour a coarse retry rhythm; the context gates total time.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(500*(attempt+1)) * time.Millisecond):
		}
	}
	return nil, lastErr
}

// listRepoFiles returns every file name in a model repository.
func (c *httpClient) listRepoFiles(ctx context.Context, repo string) ([]string, error) {
	api := fmt.Sprintf("%s/api/models/%s", Endpoint(), url.PathEscape(repo))
	resp, err := c.get(ctx, api)
	if err != nil {
		return nil, fmt.Errorf("needle: cannot list files of %s: %w", repo, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("needle: cannot list files of %s: %s", repo, resp.Status)
	}
	var payload struct {
		Siblings []struct {
			Rfilename string `json:"rfilename"`
		} `json:"siblings"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("needle: cannot parse file list of %s: %w", repo, err)
	}
	files := make([]string, 0, len(payload.Siblings))
	for _, s := range payload.Siblings {
		if s.Rfilename != "" {
			files = append(files, s.Rfilename)
		}
	}
	return files, nil
}

// resolveURL builds the download URL of a repository file.
func resolveURL(repo, filename string) string {
	return fmt.Sprintf("%s/%s/resolve/main/%s", Endpoint(), repo, filename)
}

// downloadToFile streams a URL into dest (written atomically via a .part
// file) and returns the destination path.
func (c *httpClient) downloadToFile(ctx context.Context, label, fileURL, dest string, executable bool) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	resp, err := c.get(ctx, fileURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("needle: download of %s failed: %s", fileURL, resp.Status)
	}
	part := dest + ".part"
	f, err := os.Create(part)
	if err != nil {
		return "", err
	}
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			f.Close()
			os.Remove(part)
			return "", err
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(part)
				return "", werr
			}
			done += int64(n)
			if c.onProg != nil {
				c.onProg(label, total, done)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			f.Close()
			os.Remove(part)
			return "", readErr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(part)
		return "", err
	}
	if executable {
		_ = os.Chmod(part, 0o755)
	} else {
		_ = os.Chmod(part, 0o644)
	}
	if err := os.Rename(part, dest); err != nil {
		os.Remove(part)
		return "", err
	}
	return dest, nil
}
