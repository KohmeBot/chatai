package chatai

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCleanupWorkFiles(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	now := time.Now()
	cutoff := now.Add(-7 * 24 * time.Hour)
	for name, modified := range map[string]time.Time{"123/report.txt": cutoff, "123/.tmp/old.tmp": cutoff.Add(-time.Hour), "123/new.txt": now, "456/nested/old.txt": cutoff} {
		path := filepath.Join(base, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, []byte("data"), 0600))
		require.NoError(t, os.Chtimes(path, modified, modified))
	}
	protected := filepath.Join(outside, "keep.txt")
	require.NoError(t, os.WriteFile(protected, []byte("keep"), 0600))
	require.NoError(t, os.Chtimes(protected, cutoff, cutoff))
	if err := os.Symlink(outside, filepath.Join(base, "123", "link")); err != nil {
		t.Logf("symlink unavailable: %v", err)
	}
	require.NoError(t, cleanupWorkFiles(context.Background(), base, cutoff))
	for _, name := range []string{"123/report.txt", "123/.tmp/old.tmp", "456/nested/old.txt"} {
		require.NoFileExists(t, filepath.Join(base, name))
	}
	require.FileExists(t, filepath.Join(base, "123/new.txt"))
	require.FileExists(t, protected)
	require.DirExists(t, filepath.Join(base, "456/nested"))
	require.NoError(t, cleanupWorkFiles(context.Background(), base, cutoff))
	// A longer retention period preserves a file that the default would expire.
	require.NoError(t, cleanupWorkFiles(context.Background(), outside, now.Add(-30*24*time.Hour)))
	require.FileExists(t, protected)
}

func TestWorkCleanupStopsOnClose(t *testing.T) {
	c := &ChatPlugin{conf: Config{Agent: AgentConfig{Work: WorkConfig{RetentionDays: 7}}}}
	c.startWorkCleanup(t.TempDir())
	require.NoError(t, c.Close())
	select {
	case <-c.workCleanupDone:
	default:
		t.Fatal("cleanup still running")
	}
	require.NoError(t, c.Close())
}
