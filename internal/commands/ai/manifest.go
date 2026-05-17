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

// manifestSchemaVersion is the on-disk schema version this CLI emits.
// v1 had {Version, Installed{InstalledAt, CLIVersion}}.
// v2 added the active-agent invariant + per-record CreatedFiles + a
// pair of rename-bookkeeping fields used by the now-removed
// AGENTS.md → CLAUDE.md/CONVENTIONS.md rename flow.
// v3 drops those rename fields entirely — every supported agent now
// installs its own briefing file at its own path, so there is no
// rename to reverse on uninstall. Old v2 manifests load cleanly: the
// unknown `renamed_from` / `renamed_to` keys are ignored by
// json.Unmarshal and migrated-out on the next Save.
const manifestSchemaVersion = 3

// Manifest tracks which agents have been installed in this project and
// at what CLI version. Used by `gofasta ai status`, the conflict guard
// in `gofasta ai <agent>`, and `gofasta ai uninstall`.
type Manifest struct {
	// Version of the manifest file format itself (not the gofasta CLI).
	// Bump when we change the on-disk schema so older CLIs can warn.
	Version int `json:"version"`

	// ActiveAgent is the key of the currently-installed agent, or "" if
	// none. The single-active-agent invariant is enforced in runInstall:
	// if ActiveAgent is set and a different agent is requested, the
	// install refuses without `--switch`.
	ActiveAgent string `json:"active_agent,omitempty"`

	// Installed is keyed by agent key (e.g. "claude") and records when
	// it was installed and which CLI version wrote the templates. Even
	// after `gofasta ai uninstall <key>` removes the entry, prior CLI
	// versions used to keep history here — v2 deletes the entry on
	// uninstall to keep status output focused on the active agent.
	Installed map[string]InstallRecord `json:"installed"`
}

// InstallRecord is the per-agent entry in Manifest.Installed.
type InstallRecord struct {
	InstalledAt time.Time `json:"installed_at"`
	CLIVersion  string    `json:"cli_version"`

	// CreatedFiles is every project-relative path the install wrote.
	// Uninstall walks this list to remove exactly what was added —
	// without it we'd have to guess from template enumeration, which
	// breaks if templates change between install and uninstall.
	CreatedFiles []string `json:"created_files,omitempty"`
}

// LoadManifest reads .gofasta/ai.json. Returns an empty Manifest if the
// file doesn't exist — callers can treat "fresh project" and "never
// installed any agent" identically. Auto-migrates v1 → v2 in memory
// (no file rewrite until the next Save).
func LoadManifest(projectRoot string) (*Manifest, error) {
	path := filepath.Join(projectRoot, manifestPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Manifest{
				Version:   manifestSchemaVersion,
				Installed: map[string]InstallRecord{},
			}, nil
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
	migrateManifest(&m)
	return &m, nil
}

// migrateManifest brings older on-disk schemas forward. v1 manifests
// had no ActiveAgent field; if exactly one agent was installed under v1
// we infer it as the active one. Otherwise ActiveAgent stays empty and
// the next install populates it.
func migrateManifest(m *Manifest) {
	if m.Version >= manifestSchemaVersion {
		return
	}
	if m.ActiveAgent == "" && len(m.Installed) == 1 {
		for k := range m.Installed {
			m.ActiveAgent = k
		}
	}
	m.Version = manifestSchemaVersion
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

// RecordInstall stamps an agent as installed in the manifest and marks
// it as the active agent. createdFiles is the project-relative list of
// every file the install wrote (used by uninstall to reverse cleanly).
func (m *Manifest) RecordInstall(agentKey, cliVersion string, createdFiles []string) {
	if m.Installed == nil {
		m.Installed = map[string]InstallRecord{}
	}
	m.Installed[agentKey] = InstallRecord{
		InstalledAt:  time.Now().UTC(),
		CLIVersion:   cliVersion,
		CreatedFiles: append([]string(nil), createdFiles...),
	}
	m.ActiveAgent = agentKey
}

// RecordUninstall removes an agent's record and clears ActiveAgent if
// it matches. Safe to call when the agent isn't installed (no-op).
func (m *Manifest) RecordUninstall(agentKey string) {
	if m.Installed != nil {
		delete(m.Installed, agentKey)
	}
	if m.ActiveAgent == agentKey {
		m.ActiveAgent = ""
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
