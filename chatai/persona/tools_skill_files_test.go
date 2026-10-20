package persona

import (
	"context"
	"encoding/json"
	"github.com/kohmebot/chatai/chatai/skill"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestSkillResourceSearchAndImageSource(t *testing.T) {
	d, err := skill.NewDirectory(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, d.Save("meme", "SKILL.md", "---\nname: meme\ndescription: 表情包\n---\n", false))
	for _, file := range []string{"assets/开心.png", "assets/开心.gif", "assets/other.jpg"} {
		require.NoError(t, d.Save("meme", file, "fixture", false))
	}
	s := skill.NewService(nil, nil, skill.Options{Enabled: true})
	s.Directory = d
	p := &Persona{opts: Options{Skills: s}}
	result, err := p.searchSkillFilesTool().Handler(nil, json.RawMessage(`{"query":"开心","directory":"meme","type":"file","limit":1}`))
	require.NoError(t, err)
	search := result.(skill.FileSearchResult)
	require.True(t, search.Truncated)
	require.Len(t, search.Items, 1)
	require.Equal(t, "meme/assets/开心.gif", search.Items[0].Path)
	resolved, err := p.resolveImageSource(search.Items[0].Path)
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(resolved))
	require.Equal(t, search.Items[0].AbsolutePath, resolved)
	for _, source := range []string{resolved, "https://example.com/image.png", "base64://abc", "file:///tmp/image.png"} {
		actual, err := p.resolveImageSource(source)
		require.NoError(t, err)
		require.Equal(t, source, actual)
	}
	dirs, err := d.SearchFiles(context.Background(), "ASSETS", "", "directory", 50)
	require.NoError(t, err)
	require.Len(t, dirs.Items, 1)
	require.Equal(t, "meme/assets", dirs.Items[0].Path)
	require.False(t, dirs.Truncated)
	for _, invalid := range []string{"../outside", "/outside", `meme\assets`, "meme/../outside"} {
		_, err := d.ResolveFile(invalid)
		require.Error(t, err)
		_, err = d.SearchFiles(context.Background(), "", invalid, "", 50)
		require.Error(t, err)
	}
	_, err = d.ResolveFile("meme/assets")
	require.Error(t, err)
	_, err = p.handleSendImage(nil, json.RawMessage(`{"url":"meme/missing.png"}`))
	require.ErrorContains(t, err, "resolve image path")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = d.SearchFiles(ctx, "", "", "", 50)
	require.ErrorIs(t, err, context.Canceled)
}
