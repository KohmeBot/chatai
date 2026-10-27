package skill

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMonitorPreview(t *testing.T) {
	d, err := NewDirectory(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, d.Save("demo", "SKILL.md", "---\nname: demo\ndescription: Preview\n---\n# 全文", false))
	preview, err := d.MonitorPreview("demo", "SKILL.md")
	require.NoError(t, err)
	require.Equal(t, "text", preview["kind"])
	require.Contains(t, preview["content"], "# 全文")
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(d.path, "demo", "sample.png"), png, 0600))
	preview, err = d.MonitorPreview("demo", "sample.png")
	require.NoError(t, err)
	require.Equal(t, "image", preview["kind"])
	require.Equal(t, "image/png", preview["mime"])
	require.Equal(t, base64.StdEncoding.EncodeToString(png), preview["data"])
	require.NoError(t, os.WriteFile(filepath.Join(d.path, "demo", "binary.bin"), []byte{0, 255}, 0600))
	preview, err = d.MonitorPreview("demo", "binary.bin")
	require.NoError(t, err)
	require.Equal(t, "unsupported", preview["kind"])
	for _, file := range []string{"../demo/SKILL.md", "../../secret", "C:/secret", `..\secret`, ".", "missing"} {
		_, err := d.MonitorPreview("demo", file)
		require.Error(t, err, file)
	}
	_, err = d.MonitorPreview("../demo", "SKILL.md")
	require.Error(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(d.path, "demo", "large.txt"), []byte(strings.Repeat("x", maxSkillFile+1)), 0600))
	_, err = d.MonitorPreview("demo", "large.txt")
	require.Error(t, err)
}
