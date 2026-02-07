package skill

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"openbot/internal/domain"

	"gopkg.in/yaml.v3"
)

// LoadFromDirectory loads skill definitions from YAML files in a directory.
// Files must have .yaml or .yml extension and conform to the SkillDefinition schema.
func LoadFromDirectory(dir string, logger *slog.Logger) ([]domain.SkillDefinition, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		logger.Debug("skills directory does not exist, skipping", "dir", dir)
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read skills dir: %w", err)
	}

	var skills []domain.SkillDefinition
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			logger.Warn("cannot read skill file", "path", path, "err", err)
			continue
		}

		var skill domain.SkillDefinition
		if err := yaml.Unmarshal(data, &skill); err != nil {
			logger.Warn("cannot parse skill file", "path", path, "err", err)
			continue
		}

		if skill.Name == "" {
			skill.Name = strings.TrimSuffix(name, filepath.Ext(name))
		}

		logger.Info("loaded user skill", "name", skill.Name, "path", path)
		skills = append(skills, skill)
	}

	return skills, nil
}
