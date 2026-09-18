package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testSkill = "---\nname: chat-summary\ndescription: 总结群聊\ntriggers: [总结群聊]\nnon_triggers: [总结外部文章]\n---\n# Summary\n按需读取 references/examples.md。BODY_SENTINEL\n"

func TestDirectoryProgressiveReadAndPersistence(t *testing.T) {
	base := t.TempDir()
	d, err := NewDirectory(base)
	require.NoError(t, err)
	require.NoError(t, d.Save("chat-summary", "", testSkill, false))
	require.NoError(t, d.Save("chat-summary", "references/examples.md", "REFERENCE_SENTINEL", false))
	items, err := d.List()
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "chat-summary/SKILL.md", items[0].Path)
	s := NewService(nil, nil, Options{Enabled: true})
	s.Directory = d
	matches, err := s.SearchActiveSkills(1, "总结群聊", 1)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Empty(t, matches[0].Markdown)
	require.Equal(t, []string{"read_skill"}, matches[0].RequiredTools)
	matches, err = d.Search("总结群聊，同时总结外部文章", 1)
	require.NoError(t, err)
	require.Empty(t, matches)
	body, err := d.Read("chat-summary", "")
	require.NoError(t, err)
	require.Contains(t, body, "BODY_SENTINEL")
	require.NotContains(t, body, "REFERENCE_SENTINEL")
	d, err = NewDirectory(base)
	require.NoError(t, err)
	ref, err := d.Read("chat-summary", "references/examples.md")
	require.NoError(t, err)
	require.Equal(t, "REFERENCE_SENTINEL", ref)
	require.Error(t, d.Save("chat-summary", "", testSkill, false))
	updated := strings.ReplaceAll(testSkill, "总结群聊", "翻译文章")
	require.NoError(t, d.Save("chat-summary", "", updated, true))
	matches, err = d.Search("翻译文章", 1)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	body, err = d.Read("chat-summary", "")
	require.NoError(t, err)
	require.Equal(t, updated, body)
}

func TestDirectoryRejectsInvalidContentAndTraversal(t *testing.T) {
	d, err := NewDirectory(t.TempDir())
	require.NoError(t, err)
	require.Error(t, d.Save("chat-summary", "", "# no header", false))
	require.Error(t, d.Save("wrong-name", "", testSkill, false))
	require.Error(t, d.Save("chat-summary", "references/a.md", "orphan", false))
	require.NoError(t, d.Save("chat-summary", "", testSkill, false))
	for _, file := range []string{"../outside.md", "/outside.md", "references/../../outside.md", `C:\outside.md`, `..\outside.md`, "a:stream", "."} {
		t.Run(file, func(t *testing.T) {
			require.Error(t, d.Save("chat-summary", file, "bad", true))
			_, err := d.Read("chat-summary", file)
			require.Error(t, err)
		})
	}
	require.Error(t, d.Save("../escape", "", testSkill, false))
	require.Error(t, d.Save("chat-summary", "references/large.md", strings.Repeat("x", maxSkillFile+1), false))
	require.Error(t, d.Save("chat-summary", "references/binary", string([]byte{255}), false))
	// An invalid manually installed skill must not hide other valid entries.
	require.NoError(t, os.MkdirAll(filepath.Join(d.path, "invalid"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(d.path, "invalid", "SKILL.md"), []byte("bad"), 0600))
	items, err := d.List()
	require.NoError(t, err)
	require.Len(t, items, 1)
}

func TestDirectoryRejectsSymlinkEscape(t *testing.T) {
	d, err := NewDirectory(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, d.Save("chat-summary", "", testSkill, false))
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0600))
	if err := os.Symlink(outside, filepath.Join(d.path, "chat-summary", "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err = d.Read("chat-summary", "escape/secret.md")
	require.Error(t, err)
	require.Error(t, d.Save("chat-summary", "escape/new.md", "bad", false))
	_, err = os.Stat(filepath.Join(outside, "new.md"))
	require.True(t, os.IsNotExist(err))
}
