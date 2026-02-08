package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MarketplaceConfig configures the skill marketplace.
type MarketplaceConfig struct {
	SkillDir string // local directory for installed skills
	Logger   *slog.Logger
}

// Marketplace manages the installation and discovery of skills.
type Marketplace struct {
	skillDir string
	logger   *slog.Logger
	client   *http.Client
}

// SkillManifest describes a skill available in the marketplace.
type SkillManifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	URL         string   `json:"url"`
	Tags        []string `json:"tags"`
}

// NewMarketplace creates a new skill marketplace.
func NewMarketplace(cfg MarketplaceConfig) (*Marketplace, error) {
	if cfg.SkillDir == "" {
		home, _ := os.UserHomeDir()
		cfg.SkillDir = filepath.Join(home, ".openbot", "skills")
	}
	if err := os.MkdirAll(cfg.SkillDir, 0o755); err != nil {
		return nil, fmt.Errorf("create skills directory: %w", err)
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Marketplace{
		skillDir: cfg.SkillDir,
		logger:   cfg.Logger,
		client:   &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Install downloads and installs a skill from a URL.
// The URL should point to a directory containing a skill.json manifest and skill files.
func (m *Marketplace) Install(ctx context.Context, url string) (*SkillManifest, error) {
	// Fetch the skill manifest.
	manifestURL := strings.TrimRight(url, "/") + "/skill.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch skill manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest not found at %s (status %d)", manifestURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB max
	if err != nil {
		return nil, err
	}

	var manifest SkillManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("invalid skill manifest: %w", err)
	}

	if manifest.Name == "" {
		return nil, fmt.Errorf("skill manifest missing name")
	}

	// Create skill directory.
	skillPath := filepath.Join(m.skillDir, manifest.Name)
	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		return nil, err
	}

	// Save manifest.
	if err := os.WriteFile(filepath.Join(skillPath, "skill.json"), body, 0o644); err != nil {
		return nil, err
	}

	m.logger.Info("skill installed", "name", manifest.Name, "version", manifest.Version)
	return &manifest, nil
}

// Uninstall removes an installed skill.
func (m *Marketplace) Uninstall(name string) error {
	skillPath := filepath.Join(m.skillDir, name)
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		return fmt.Errorf("skill %q not installed", name)
	}
	return os.RemoveAll(skillPath)
}

// ListInstalled returns all locally installed skills.
func (m *Marketplace) ListInstalled() ([]SkillManifest, error) {
	entries, err := os.ReadDir(m.skillDir)
	if err != nil {
		return nil, err
	}

	var skills []SkillManifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(m.skillDir, entry.Name(), "skill.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}
		var manifest SkillManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}
		skills = append(skills, manifest)
	}

	return skills, nil
}

// GetSkillDir returns the base directory for installed skills.
func (m *Marketplace) GetSkillDir() string {
	return m.skillDir
}
