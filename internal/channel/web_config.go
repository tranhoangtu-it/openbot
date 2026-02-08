package channel

import (
	"encoding/json"
	"io"
	"net/http"

	"openbot/internal/config"
)

// handleSettings renders the settings page.
func (w *Web) handleSettings(rw http.ResponseWriter, r *http.Request) {
	if err := w.tmpl.ExecuteTemplate(rw, "settings.html", map[string]any{
		"Title":       "OpenBot Settings",
		"Description": "OpenBot settings — configure providers, channels, security, and tools.",
	}); err != nil {
		w.logger.Error("template error", "template", "settings", "err", err)
	}
}

// handleGetConfig returns the current config (with secrets masked).
func (w *Web) handleGetConfig(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")

	w.cfgMu.RLock()
	cfg := w.cfg
	w.cfgMu.RUnlock()

	if cfg == nil {
		rw.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(rw).Encode(map[string]string{"error": "config not loaded"})
		return
	}
	sanitized := config.Sanitize(cfg)
	json.NewEncoder(rw).Encode(sanitized)
}

// handleUpdateConfig applies partial or full config updates (in-memory only).
func (w *Web) handleUpdateConfig(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")

	w.cfgMu.Lock()
	defer w.cfgMu.Unlock()

	if w.cfg == nil {
		rw.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(rw).Encode(map[string]string{"error": "config not loaded"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil {
		rw.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(rw).Encode(map[string]string{"error": "read body: " + err.Error()})
		return
	}
	defer r.Body.Close()

	// Try partial update first: { "path": "general.defaultProvider", "value": "ollama" }
	var partial struct {
		Path  string `json:"path"`
		Value any    `json:"value"`
	}
	if err := json.Unmarshal(body, &partial); err == nil && partial.Path != "" {
		if err := config.SetByPath(w.cfg, partial.Path, partial.Value); err != nil {
			rw.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(rw).Encode(map[string]string{"error": err.Error()})
			return
		}
		if err := config.Validate(w.cfg); err != nil {
			rw.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(rw).Encode(map[string]string{"error": "validation: " + err.Error()})
			return
		}
		w.logger.Info("config updated via path", "path", partial.Path, "value", partial.Value)
		json.NewEncoder(rw).Encode(map[string]string{"status": "updated", "path": partial.Path})
		return
	}

	// Full config update — validate before applying.
	var candidate config.Config
	if err := json.Unmarshal(body, &candidate); err != nil {
		rw.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(rw).Encode(map[string]string{"error": "invalid config: " + err.Error()})
		return
	}
	if err := config.Validate(&candidate); err != nil {
		rw.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(rw).Encode(map[string]string{"error": "validation: " + err.Error()})
		return
	}
	*w.cfg = candidate

	w.logger.Info("config updated (full)")
	json.NewEncoder(rw).Encode(map[string]string{"status": "updated"})
}

// handleSaveConfig persists the current in-memory config to disk.
func (w *Web) handleSaveConfig(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")

	w.cfgMu.RLock()
	cfg := w.cfg
	cfgPath := w.cfgPath
	w.cfgMu.RUnlock()

	if cfg == nil || cfgPath == "" {
		rw.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(rw).Encode(map[string]string{"error": "config not available"})
		return
	}

	if err := config.Save(cfgPath, cfg); err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(rw).Encode(map[string]string{"error": "save failed: " + err.Error()})
		return
	}

	w.logger.Info("config saved to disk", "path", cfgPath)
	json.NewEncoder(rw).Encode(map[string]string{"status": "saved", "path": cfgPath})
}
