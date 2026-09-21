package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjectSnapshot(t *testing.T) {
	d, err := NewDirectory(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, d.Save("chat-summary", "", testSkill, false))
	for file, content := range map[string]string{"scripts/main.py": "import helper", "scripts/helper.py": "value = 42", "assets/input.json": "{}", "requirements.txt": "requests"} {
		require.NoError(t, d.Save("chat-summary", file, content, false))
	}
	files, err := d.Files(context.Background(), "chat-summary")
	require.NoError(t, err)
	require.Len(t, files, 5)
	dir, cleanup, err := d.Snapshot(context.Background(), "chat-summary", "scripts/main.py")
	require.NoError(t, err)
	defer cleanup()
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(dir, file.Path))
		require.NoError(t, err)
		original, err := d.Read("chat-summary", file.Path)
		require.NoError(t, err)
		require.Equal(t, original, string(data))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts/main.py"), []byte("changed"), 0600))
	original, err := d.Read("chat-summary", "scripts/main.py")
	require.NoError(t, err)
	require.Equal(t, "import helper", original)
	cleanup()
	_, err = os.Stat(dir)
	require.True(t, os.IsNotExist(err))
}

func TestProjectRejectsInvalidAndOversizedSources(t *testing.T) {
	d, err := NewDirectory(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, d.Save("chat-summary", "", testSkill, false))
	for _, entry := range []string{"", "../escape.py", "/tmp/a.py", "C:/a.py", `scripts\a.py`, "SKILL.md", "missing.py"} {
		_, _, err := d.Snapshot(context.Background(), "chat-summary", entry)
		require.Error(t, err, entry)
	}
	require.NoError(t, d.Save("chat-summary", "main.py", "print(1)", false))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = d.Snapshot(ctx, "chat-summary", "main.py")
	require.ErrorIs(t, err, context.Canceled)
	for _, name := range []string{"a.txt", "b.txt"} {
		require.NoError(t, d.Save("chat-summary", name, strings.Repeat("x", maxSkillFile), false))
	}
	_, _, err = d.Snapshot(context.Background(), "chat-summary", "main.py")
	require.ErrorContains(t, err, "10 MiB")
}

func TestProjectRejectsSymlinks(t *testing.T) {
	d, err := NewDirectory(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, d.Save("chat-summary", "", testSkill, false))
	require.NoError(t, d.Save("chat-summary", "main.py", "print(1)", false))
	if err := os.Symlink(filepath.Join(d.path, "chat-summary", "main.py"), filepath.Join(d.path, "chat-summary", "link.py")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, _, err = d.Snapshot(context.Background(), "chat-summary", "main.py")
	require.ErrorContains(t, err, "non-regular")
}
