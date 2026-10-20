package skill

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type FileMatch struct {
	Path         string `json:"path"`
	AbsolutePath string `json:"absolute_path"`
	Type         string `json:"type"`
	Size         int64  `json:"size"`
}

type FileSearchResult struct {
	Items     []FileMatch `json:"items"`
	Truncated bool        `json:"truncated"`
}

func validAssetPath(p string) bool {
	return p != "" && !strings.ContainsAny(p, "\\:") && filepath.IsLocal(p) && path.Clean(p) == p
}

// ResolveFile accepts a path relative to the skills root, never the process cwd.
func (d *Directory) ResolveFile(p string) (string, error) {
	if !validAssetPath(p) {
		return "", fmt.Errorf("path must be relative to the skills directory")
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return "", err
	}
	defer root.Close()
	info, err := root.Stat(p)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular skill file: %s", p)
	}
	return filepath.Abs(filepath.Join(d.path, filepath.FromSlash(p)))
}

// SearchFiles searches resource paths without parsing routing metadata or bodies.
func (d *Directory) SearchFiles(ctx context.Context, query, dir, kind string, limit int) (FileSearchResult, error) {
	result := FileSearchResult{Items: make([]FileMatch, 0)}
	if dir == "" {
		dir = "."
	}
	if !validAssetPath(dir) {
		return result, fmt.Errorf("directory must be relative to the skills directory")
	}
	if kind != "" && kind != "file" && kind != "directory" {
		return result, fmt.Errorf("type must be file or directory")
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return result, fmt.Errorf("limit must be 1..200")
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return result, err
	}
	defer root.Close()
	sub, err := root.OpenRoot(dir)
	if err != nil {
		return result, err
	}
	defer sub.Close()
	query = strings.ToLower(strings.TrimSpace(query))
	err = fs.WalkDir(sub.FS(), ".", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == "." || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		relative := path.Join(dir, p)
		entryKind := "file"
		if entry.IsDir() {
			entryKind = "directory"
		}
		if (kind != "" && kind != entryKind) || !strings.Contains(strings.ToLower(relative), query) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return nil
		}
		if len(result.Items) == limit {
			result.Truncated = true
			return fs.SkipAll
		}
		absolute, err := filepath.Abs(filepath.Join(d.path, filepath.FromSlash(relative)))
		if err != nil {
			return err
		}
		result.Items = append(result.Items, FileMatch{Path: relative, AbsolutePath: absolute, Type: entryKind, Size: info.Size()})
		return nil
	})
	return result, err
}
