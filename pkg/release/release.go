// Package release contains function to help finding things out about a given
// embedded cluster release. It is being kept here so if we decide to manage
// releases in a different way, we can easily change it.
package release

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

var (
	ghurl = "https://github.com/replicatedhq/embedded-cluster/releases/download/v%s/metadata.json"
	cache = map[string]*Meta{}
	mutex = sync.Mutex{}
)

// Versions holds a list of add-on versions.
type Versions struct {
	AdminConsole            string
	EmbeddedClusterOperator string
	Installer               string
	Kubernetes              string
	OpenEBS                 string
}

// Meta represents the components of a given embedded cluster release. This
// is read directly from GitHub releases page.
type Meta struct {
	Versions     Versions
	K0sSHA       string
	K0sBinaryURL string
}

// MetadataFor reads metadata for a given release. Goes to GitHub releases page
// and reads metadata.json file.
func MetadataFor(ctx context.Context, version string) (*Meta, error) {
	mutex.Lock()
	defer mutex.Unlock()
	version = strings.TrimPrefix(version, "v")
	if meta, ok := cache[version]; ok {
		return meta, nil
	}
	url := fmt.Sprintf(ghurl, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get bundle from %q: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get bundle from %q: %s", url, resp.Status)
	}
	var meta Meta
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("failed to decode bundle: %w", err)
	}
	cache[version] = &meta
	return &meta, nil
}
