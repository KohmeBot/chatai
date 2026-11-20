package skill

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
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

// ReadAsset reads a binary resource within an enabled directory skill. It
// accepts either form of path returned by SearchFiles for such resources.
func (s *Service) ReadAsset(source string, maxBytes int64) ([]byte, error) {
	if s == nil || !s.opts.Enabled || s.Directory == nil {
		return nil, fmt.Errorf("Skills 未启用")
	}
	if maxBytes <= 0 || maxBytes == math.MaxInt64 {
		return nil, fmt.Errorf("invalid skill asset size limit")
	}
	d := s.Directory
	relative := source
	if filepath.IsAbs(source) {
		if filepath.Clean(source) != filepath.FromSlash(source) {
			return nil, fmt.Errorf("invalid skill asset path")
		}
		base, err := filepath.Abs(d.path)
		if err != nil {
			return nil, err
		}
		relative, err = filepath.Rel(base, source)
		if err != nil {
			return nil, err
		}
		relative = filepath.ToSlash(relative)
	}
	name, file, nested := strings.Cut(relative, "/")
	if !nested || !validAssetPath(relative) {
		return nil, fmt.Errorf("asset path must be inside a skill directory")
	}
	if err := s.CheckEnabled(name); err != nil {
		return nil, err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	root, err := d.openProject(name)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	data, err := readLimitedSkillFile(root, file, maxBytes)
	if errors.Is(err, errSkillFileTooLarge) {
		return nil, fmt.Errorf("skill asset exceeds the %d-byte limit", maxBytes)
	}
	return data, err
}

// SearchFiles searches resource paths without parsing routing metadata or bodies.
func (d *Directory) SearchFiles(ctx context.Context, query, dir, kind string, limit int) (FileSearchResult, error) {
	return d.searchFiles(ctx, query, dir, kind, limit, nil)
}
func (s *Service) SearchFiles(ctx context.Context, query, dir, kind string, limit int) (FileSearchResult, error) {
	keys, err := s.DisabledKeys()
	if err != nil {
		return FileSearchResult{}, err
	}
	if keys[ResidentKey(strings.Split(dir, "/")[0], 0)] {
		return FileSearchResult{}, fmt.Errorf("Skill 已禁用")
	}
	return s.Directory.searchFiles(ctx, query, dir, kind, limit, keys)
}
func (d *Directory) searchFiles(ctx context.Context, query, dir, kind string, limit int, disabled map[string]bool) (FileSearchResult, error) {
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
		if disabled[ResidentKey(strings.Split(relative, "/")[0], 0)] {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
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
