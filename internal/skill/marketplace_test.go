package skill

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewMarketplace(t *testing.T) {
	dir := t.TempDir()
	mp, err := NewMarketplace(MarketplaceConfig{
		SkillDir: filepath.Join(dir, "skills"),
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	if err != nil {
		t.Fatal(err)
	}
	if mp.GetSkillDir() == "" {
		t.Error("skill dir should be set")
	}
}

func TestMarketplace_ListInstalled_Empty(t *testing.T) {
	dir := t.TempDir()
	mp, _ := NewMarketplace(MarketplaceConfig{
		SkillDir: dir,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})

	skills, err := mp.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}
}

func TestMarketplace_Install(t *testing.T) {
	manifest := SkillManifest{
		Name:        "test-skill",
		Version:     "1.0",
		Description: "A test skill",
		Author:      "test",
	}
	manifestJSON, _ := json.Marshal(manifest)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/skill.json" {
			w.Write(manifestJSON)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	mp, _ := NewMarketplace(MarketplaceConfig{
		SkillDir: dir,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})

	installed, err := mp.Install(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Name != "test-skill" {
		t.Errorf("expected test-skill, got %s", installed.Name)
	}

	// Verify installed
	skills, _ := mp.ListInstalled()
	if len(skills) != 1 {
		t.Errorf("expected 1 installed skill, got %d", len(skills))
	}
}

func TestMarketplace_Uninstall(t *testing.T) {
	dir := t.TempDir()
	mp, _ := NewMarketplace(MarketplaceConfig{
		SkillDir: dir,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})

	// Create a skill manually
	skillDir := filepath.Join(dir, "test-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "skill.json"), []byte(`{"name":"test-skill"}`), 0o644)

	err := mp.Uninstall("test-skill")
	if err != nil {
		t.Fatal(err)
	}

	// Should be gone
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("skill dir should be removed")
	}
}

func TestMarketplace_Uninstall_NotInstalled(t *testing.T) {
	dir := t.TempDir()
	mp, _ := NewMarketplace(MarketplaceConfig{
		SkillDir: dir,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})

	err := mp.Uninstall("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent skill")
	}
}

func TestMarketplace_Install_InvalidManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	dir := t.TempDir()
	mp, _ := NewMarketplace(MarketplaceConfig{
		SkillDir: dir,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})

	_, err := mp.Install(context.Background(), server.URL)
	if err == nil {
		t.Error("expected error for invalid manifest")
	}
}

func TestMarketplace_Install_MissingName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"1.0"}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	mp, _ := NewMarketplace(MarketplaceConfig{
		SkillDir: dir,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})

	_, err := mp.Install(context.Background(), server.URL)
	if err == nil {
		t.Error("expected error for missing name")
	}
}
