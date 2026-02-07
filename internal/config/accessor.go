package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// GetByPath retrieves a config value by dot-notation path (e.g. "general.workspace").
func GetByPath(cfg *Config, path string) (any, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}

	parts := strings.Split(path, ".")
	var current any = m
	for _, key := range parts {
		switch v := current.(type) {
		case map[string]any:
			val, ok := v[key]
			if !ok {
				return nil, fmt.Errorf("key not found: %s", path)
			}
			current = val
		case []any:
			idx, err := strconv.Atoi(key)
			if err != nil || idx < 0 || idx >= len(v) {
				return nil, fmt.Errorf("invalid array index: %s", key)
			}
			current = v[idx]
		default:
			return nil, fmt.Errorf("cannot traverse into %T at %s", current, key)
		}
	}
	return current, nil
}

// SetByPath sets a config value by dot-notation path and returns updated config.
func SetByPath(cfg *Config, path string, value any) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}

	// Navigate to parent and set value
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return fmt.Errorf("empty path")
	}

	parent := m
	for i := 0; i < len(parts)-1; i++ {
		child, ok := parent[parts[i]]
		if !ok {
			newMap := make(map[string]any)
			parent[parts[i]] = newMap
			parent = newMap
			continue
		}
		childMap, ok := child.(map[string]any)
		if !ok {
			return fmt.Errorf("cannot traverse into %T at %s", child, parts[i])
		}
		parent = childMap
	}

	lastKey := parts[len(parts)-1]

	// Try to parse value as proper type
	parent[lastKey] = parseValue(value)

	newData, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(newData, cfg)
}

// parseValue tries to convert string values to appropriate Go types.
func parseValue(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}

	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}

	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}

	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}

	return s
}

// Sanitize returns a copy of the config with sensitive values masked.
func Sanitize(cfg *Config) *Config {
	data, err := json.Marshal(cfg)
	if err != nil {
		return cfg // Return original on marshal error
	}
	var copy Config
	if err := json.Unmarshal(data, &copy); err != nil {
		return cfg
	}

	for name, prov := range copy.Providers {
		if prov.APIKey != "" {
			prov.APIKey = maskString(prov.APIKey)
		}
		copy.Providers[name] = prov
	}

	if copy.Channels.Telegram.Token != "" {
		copy.Channels.Telegram.Token = maskString(copy.Channels.Telegram.Token)
	}

	if copy.Channels.Web.Auth.PasswordHash != "" {
		copy.Channels.Web.Auth.PasswordHash = "***"
	}

	if copy.Tools.Web.SearchAPIKey != "" {
		copy.Tools.Web.SearchAPIKey = maskString(copy.Tools.Web.SearchAPIKey)
	}

	return &copy
}

// maskString shows first 4 and last 4 chars, masks the rest.
func maskString(s string) string {
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "****" + s[len(s)-4:]
}

// ListPaths returns all settable config paths with their current values.
func ListPaths(cfg *Config) map[string]any {
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	result := make(map[string]any)
	flattenMap("", m, result)
	return result
}

func flattenMap(prefix string, m map[string]any, result map[string]any) {
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]any:
			flattenMap(path, val, result)
		default:
			result[path] = val
		}
	}
}
