package chatai

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
)

func (c *ChatPlugin) startWorkCleanup(path string) {
	ctx, cancel := context.WithCancel(context.Background())
	c.workCleanupCancel = cancel
	c.workCleanupDone = make(chan struct{})
	retention := time.Duration(c.conf.Agent.Work.RetentionDays) * 24 * time.Hour
	go func() {
		defer close(c.workCleanupDone)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			if err := cleanupWorkFiles(ctx, path, time.Now().Add(-retention)); err != nil && ctx.Err() == nil {
				logrus.Warnf("[ChatAI][Work] 清理过期文件失败: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// Root confines removal to the workspace; WalkDir never follows symlinks.
// Keep directories so idle groups and configured command paths remain usable.
func cleanupWorkFiles(ctx context.Context, path string, cutoff time.Time) error {
	root, err := os.OpenRoot(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	var failures error
	walkErr := fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			failures = errors.Join(failures, err)
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := root.Lstat(filepath.FromSlash(name))
		if err == nil && !info.IsDir() && !info.ModTime().After(cutoff) {
			err = root.Remove(filepath.FromSlash(name))
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			failures = errors.Join(failures, err)
		}
		return nil
	})
	return errors.Join(failures, walkErr)
}
