package skill

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const maxProjectFiles = 1000
const maxProjectBytes = 10 * 1024 * 1024

type FileInfo struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// openProject confines all reads to this skill, including during concurrent edits.
func (d *Directory) openProject(name string) (*os.Root, error) {
	if _, err := skillPath(name, "SKILL.md"); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("skill must be a directory, not a symlink")
	}
	sub, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	f, err := sub.Open("SKILL.md")
	if err == nil {
		_, err = readMetadata(f, name)
		f.Close()
	}
	if err != nil {
		sub.Close()
		return nil, err
	}
	return sub, nil
}

func projectFiles(ctx context.Context, root *os.Root) ([]FileInfo, error) {
	files := make([]FileInfo, 0)
	var size int64
	err := fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular skill file: %s", path)
		}
		size += info.Size()
		if len(files) >= maxProjectFiles || size > maxProjectBytes {
			return fmt.Errorf("skill project exceeds 1000 files or 10 MiB")
		}
		files = append(files, FileInfo{Path: path, Size: info.Size()})
		return nil
	})
	return files, err
}

func (d *Directory) Files(ctx context.Context, name string) ([]FileInfo, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	root, err := d.openProject(name)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return projectFiles(ctx, root)
}

// Snapshot creates a private, bounded copy. The caller must invoke cleanup.
// Passing a snapshot to pyrunner preserves imports/resources without exposing
// the live skill directory to its path-based SDK or to the Python process.
func (d *Directory) Snapshot(ctx context.Context, name, entry string) (dir string, cleanup func(), err error) {
	entry, err = skillPath(name, entry)
	if err != nil {
		return "", nil, err
	}
	if !strings.HasSuffix(entry, ".py") {
		return "", nil, fmt.Errorf("entry must be a .py file")
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	root, err := d.openProject(name)
	if err != nil {
		return "", nil, err
	}
	defer root.Close()
	files, err := projectFiles(ctx, root)
	if err != nil {
		return "", nil, err
	}
	found := false
	for _, file := range files {
		if file.Path == entry {
			found = true
		}
	}
	if !found {
		return "", nil, fmt.Errorf("script %q not found in skill", entry)
	}
	dir, err = os.MkdirTemp("", "chatai-skill-*")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	defer func() {
		if err != nil {
			cleanup()
		}
	}()
	var copied int64
	for _, file := range files {
		if err = ctx.Err(); err != nil {
			return
		}
		var data []byte
		data, err = readProjectFile(root, file.Path, maxProjectBytes-copied)
		if err != nil {
			return
		}
		copied += int64(len(data))
		destination := filepath.Join(dir, filepath.FromSlash(file.Path))
		if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return
		}
		if err = os.WriteFile(destination, data, 0600); err != nil {
			return
		}
	}
	return
}

func readProjectFile(root *os.Root, path string, limit int64) ([]byte, error) {
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("non-regular skill file: %s", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("skill project exceeds 10 MiB")
	}
	return data, nil
}
