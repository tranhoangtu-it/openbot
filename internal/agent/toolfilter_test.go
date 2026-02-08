package agent

import (
	"testing"

	"openbot/internal/domain"
)

func TestToolFilter_NilFilter(t *testing.T) {
	var tf *ToolFilter
	if !tf.IsAllowed("shell") {
		t.Error("nil filter should allow everything")
	}
	if !tf.IsEmpty() {
		t.Error("nil filter should be empty")
	}
}

func TestToolFilter_EmptyFilter(t *testing.T) {
	tf := NewToolFilter(nil, nil)
	if !tf.IsAllowed("shell") {
		t.Error("empty filter should allow everything")
	}
	if !tf.IsEmpty() {
		t.Error("empty filter should be empty")
	}
}

func TestToolFilter_AllowList(t *testing.T) {
	tf := NewToolFilter([]string{"shell", "read_file"}, nil)

	if !tf.IsAllowed("shell") {
		t.Error("shell should be allowed")
	}
	if !tf.IsAllowed("read_file") {
		t.Error("read_file should be allowed")
	}
	if tf.IsAllowed("web_fetch") {
		t.Error("web_fetch should NOT be allowed")
	}
}

func TestToolFilter_DenyList(t *testing.T) {
	tf := NewToolFilter(nil, []string{"shell"})

	if tf.IsAllowed("shell") {
		t.Error("shell should be denied")
	}
	if !tf.IsAllowed("read_file") {
		t.Error("read_file should be allowed")
	}
}

func TestToolFilter_DenyOverridesAllow(t *testing.T) {
	tf := NewToolFilter([]string{"shell", "read_file"}, []string{"shell"})

	if tf.IsAllowed("shell") {
		t.Error("shell should be denied (deny overrides allow)")
	}
	if !tf.IsAllowed("read_file") {
		t.Error("read_file should be allowed")
	}
}

func TestToolFilter_FilterDefinitions(t *testing.T) {
	tf := NewToolFilter([]string{"shell", "read_file"}, nil)

	defs := []domain.ToolDefinition{
		{Name: "shell", Description: "Execute commands"},
		{Name: "read_file", Description: "Read a file"},
		{Name: "web_fetch", Description: "Fetch URL"},
		{Name: "write_file", Description: "Write a file"},
	}

	filtered := tf.FilterDefinitions(defs)
	if len(filtered) != 2 {
		t.Errorf("expected 2 definitions after filtering, got %d", len(filtered))
	}
	for _, d := range filtered {
		if d.Name != "shell" && d.Name != "read_file" {
			t.Errorf("unexpected tool in filtered list: %s", d.Name)
		}
	}
}

func TestToolFilter_FilterDefinitions_NilFilter(t *testing.T) {
	var tf *ToolFilter
	defs := []domain.ToolDefinition{
		{Name: "shell"}, {Name: "web_fetch"},
	}
	filtered := tf.FilterDefinitions(defs)
	if len(filtered) != len(defs) {
		t.Error("nil filter should return all definitions")
	}
}

func TestToolFilter_FilterDefinitions_EmptyDefs(t *testing.T) {
	tf := NewToolFilter([]string{"shell"}, nil)
	filtered := tf.FilterDefinitions(nil)
	if len(filtered) != 0 {
		t.Error("empty definitions should return empty")
	}
}

func TestToolFilter_IsEmpty_WithRules(t *testing.T) {
	tf := NewToolFilter([]string{"shell"}, nil)
	if tf.IsEmpty() {
		t.Error("filter with allow rules should not be empty")
	}

	tf2 := NewToolFilter(nil, []string{"shell"})
	if tf2.IsEmpty() {
		t.Error("filter with deny rules should not be empty")
	}
}
