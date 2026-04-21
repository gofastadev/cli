package ai

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
)

// manifestPath is the relative path to the installer's bookkeeping file.
// Stored under .gofasta/ rather than the root so it doesn't clutter the
// project tree — humans rarely look at it, but agents can consult it
// to know which configs are present and at what version.
const manifestPath = ".gofasta/ai.json"

// Manifest tracks which agents have been installed in this project and
// at what CLI version. Used by `gofasta ai status` and by the upgrade
// flow in the future to diff installed config vs latest.
type Manifest struct {
	// Version of the manifest file format itself (not the gofasta CLI).
	// Bump when we change the on-disk schema so older CLIs can warn.
	Version int `json:"version"`

	// Installed is keyed by agent key (e.g. "claude") and records when
	// it was installed and which CLI version wrote the templates. A
	// later CLI version can detect "older templates installed" and
	// offer an upgrade.
	Installed map[string]InstallRecord `json:"installed"`
}

// InstallRecord is the per-agent entry in Manifest.Installed.
type InstallRecord struct {
	InstalledAt time.Time `json:"installed_at"`
	CLIVersion  string    `json:"cli_version"`
}

// LoadManifest reads .gofasta/ai.json. Returns an empty Manifest if the
// file doesn't exist — callers can treat "fresh project" and "never
// installed any agent" identically.
func LoadManifest(projectRoot string) (*Manifest, error) {
	path := filepath.Join(projectRoot, manifestPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Manifest{Version: 1, Installed: map[string]InstallRecord{}}, nil
		}
		return nil, clierr.Wrap(clierr.CodeAIManifestIO, err,
			"could not read "+manifestPath)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, clierr.Wrap(clierr.CodeAIManifestIO, err,
			manifestPath+" is not valid JSON")
	}
	if m.Installed == nil {
		m.Installed = map[string]InstallRecord{}
	}
	return &m, nil
}

// manifestMarshal is a package-level seam for json.MarshalIndent so
// tests can force a serialize error. json.MarshalIndent never fails on
// a valid Manifest, so without a seam this branch is unreachable.
var manifestMarshal = json.MarshalIndent

// Save writes the manifest atomically (write temp file + rename) so a
// crashed CLI process never leaves a half-written file on disk.
func (m *Manifest) Save(projectRoot string) error {
	dir := filepath.Join(projectRoot, ".gofasta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return clierr.Wrap(clierr.CodeAIManifestIO, err,
			"could not create .gofasta/ directory")
	}
	data, err := manifestMarshal(m, "", "  ")
	if err != nil {
		return clierr.Wrap(clierr.CodeAIManifestIO, err,
			"could not serialize manifest")
	}
	tmp := filepath.Join(projectRoot, manifestPath+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return clierr.Wrap(clierr.CodeAIManifestIO, err,
			"could not write "+manifestPath)
	}
	if err := os.Rename(tmp, filepath.Join(projectRoot, manifestPath)); err != nil {
		return clierr.Wrap(clierr.CodeAIManifestIO, err,
			"could not rename temp manifest into place")
	}
	return nil
}

// RecordInstall stamps an agent as installed in the manifest and saves.
func (m *Manifest) RecordInstall(agentKey, cliVersion string) {
	if m.Installed == nil {
		m.Installed = map[string]InstallRecord{}
	}
	m.Installed[agentKey] = InstallRecord{
		InstalledAt: time.Now().UTC(),
		CLIVersion:  cliVersion,
	}
}

// InstalledKeys returns every installed agent key, sorted. Used by
// `gofasta ai status`.
func (m *Manifest) InstalledKeys() []string {
	keys := make([]string, 0, len(m.Installed))
	for k := range m.Installed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
