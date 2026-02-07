package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"openbot/internal/domain"
)

// resolvePath resolves a file path relative to the workspace and prevents traversal.
func resolvePath(workspace, path string) (string, error) {
	path = strings.TrimSpace(path)
	if !filepath.IsAbs(path) && workspace != "" {
		path = filepath.Join(workspace, path)
	}
	resolved, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	// Enforce workspace restriction to prevent directory traversal.
	if workspace != "" {
		wsAbs, err := filepath.Abs(workspace)
		if err != nil {
			return "", fmt.Errorf("resolve workspace: %w", err)
		}
		if !strings.HasPrefix(resolved, wsAbs+string(filepath.Separator)) && resolved != wsAbs {
			return "", fmt.Errorf("path %q is outside workspace %q", resolved, wsAbs)
		}
	}
	return resolved, nil
}

// --- ReadFileTool ---

// ReadFileTool reads the contents of a file inside the workspace.
type ReadFileTool struct {
	workspace string
}

func NewReadFileTool(workspace string) *ReadFileTool {
	return &ReadFileTool{workspace: workspace}
}

func (t *ReadFileTool) Name() string        { return "read_file" }
func (t *ReadFileTool) Description() string { return "Read the contents of a file. Provide the file path relative to workspace or absolute." }
func (t *ReadFileTool) Parameters() map[string]any {
	return ToolParameters(
		map[string]Param{
			"path": {Type: "string", Description: "File path to read (relative to workspace or absolute)"},
		},
		[]string{"path"},
	)
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path := ArgsString(args, "path")
	if path == "" {
		return "", fmt.Errorf("missing argument: path")
	}
	resolved, err := resolvePath(t.workspace, path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return string(data), nil
}

// --- WriteFileTool ---

// WriteFileTool writes content to a file, creating parent directories as needed.
type WriteFileTool struct {
	workspace string
}

func NewWriteFileTool(workspace string) *WriteFileTool {
	return &WriteFileTool{workspace: workspace}
}

func (t *WriteFileTool) Name() string        { return "write_file" }
func (t *WriteFileTool) Description() string { return "Write content to a file. Creates the file if it does not exist; overwrites if it exists." }
func (t *WriteFileTool) Parameters() map[string]any {
	return ToolParameters(
		map[string]Param{
			"path":    {Type: "string", Description: "File path to write (relative to workspace or absolute)"},
			"content": {Type: "string", Description: "Content to write to the file"},
		},
		[]string{"path", "content"},
	)
}

func (t *WriteFileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path := ArgsString(args, "path")
	content := ArgsString(args, "content")
	if path == "" {
		return "", fmt.Errorf("missing argument: path")
	}
	resolved, err := resolvePath(t.workspace, path)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(resolved)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}
	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(content), resolved), nil
}

// --- ListDirTool ---

// ListDirTool lists files and directories at a given path.
type ListDirTool struct {
	workspace string
}

func NewListDirTool(workspace string) *ListDirTool {
	return &ListDirTool{workspace: workspace}
}

func (t *ListDirTool) Name() string        { return "list_dir" }
func (t *ListDirTool) Description() string { return "List files and directories at the given path. Use '.' or empty for current directory." }
func (t *ListDirTool) Parameters() map[string]any {
	return ToolParameters(
		map[string]Param{
			"path": {Type: "string", Description: "Directory path to list (use '.' for current directory)"},
		},
		[]string{},
	)
}

func (t *ListDirTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path := ArgsString(args, "path")
	if path == "" {
		path = "."
	}
	resolved, err := resolvePath(t.workspace, path)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return "", fmt.Errorf("list dir: %w", err)
	}
	var lines []string
	for _, e := range entries {
		info, err := e.Info()
		size := ""
		if err == nil && info != nil && !e.IsDir() {
			size = fmt.Sprintf(" %d", info.Size())
		}
		lines = append(lines, e.Name()+size)
	}
	return strings.Join(lines, "\n"), nil
}

// Compile-time interface checks.
var (
	_ domain.Tool = (*ReadFileTool)(nil)
	_ domain.Tool = (*WriteFileTool)(nil)
	_ domain.Tool = (*ListDirTool)(nil)
)
