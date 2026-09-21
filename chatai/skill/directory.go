package skill

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

const maxSkillFile = 5 * 1024 * 1024

var directoryName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// Directory indexes only front matter; bodies and references are read on demand.
type Directory struct {
	path string
	mu   sync.RWMutex
}

type Metadata struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description"`
	Triggers    []string `yaml:"triggers" json:"triggers,omitempty"`
	NonTriggers []string `yaml:"non_triggers" json:"non_triggers,omitempty"`
	Path        string   `yaml:"-" json:"path"`
}

func NewDirectory(dataDir string) (*Directory, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("empty data directory")
	}
	dir := filepath.Join(dataDir, "skills")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Directory{path: dir}, nil
}

func readMetadata(r io.Reader, name string) (Metadata, error) {
	scanner := bufio.NewScanner(io.LimitReader(r, 16*1024))
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return Metadata{}, errors.New("SKILL.md requires YAML front matter")
	}
	var header strings.Builder
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "---" {
			var m Metadata
			if err := yaml.Unmarshal([]byte(header.String()), &m); err != nil {
				return m, err
			}
			if m.Name != name || !directoryName.MatchString(m.Name) || strings.TrimSpace(m.Description) == "" || len(m.Description) > 2000 {
				return m, errors.New("name must match folder; description is required (max 2000 bytes)")
			}
			m.Path = name + "/SKILL.md"
			return m, nil
		}
		header.WriteString(scanner.Text() + "\n")
	}
	return Metadata{}, errors.New("invalid or oversized front matter")
}

func (d *Directory) List() ([]Metadata, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	result := make([]Metadata, 0)
	for _, e := range entries {
		if !e.IsDir() || !directoryName.MatchString(e.Name()) {
			continue
		}
		f, err := root.Open(e.Name() + "/SKILL.md")
		if err != nil {
			logrus.Warnf("[Skill][目录跳过] %s: %v", e.Name(), err)
			continue
		}
		m, err := readMetadata(f, e.Name())
		f.Close()
		if err != nil {
			logrus.Warnf("[Skill][目录跳过] %s: %v", e.Name(), err)
			continue
		}
		result = append(result, m)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (d *Directory) Search(query string, limit int) ([]agent.SkillMatch, error) {
	items, err := d.List()
	if err != nil {
		return nil, err
	}
	type hit struct {
		m     Metadata
		score float64
	}
	var hits []hit
	for _, m := range items {
		r := Record{Name: m.Name, Description: m.Description, TriggersJSON: encodeStrings(m.Triggers)}
		score := matchScore(query, r)
		if score >= .22 && !matchesNegativeTrigger(query, m.NonTriggers) {
			hits = append(hits, hit{m, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	limit = agent.SkillSearchLimit(limit)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	result := make([]agent.SkillMatch, 0, len(hits))
	for _, h := range hits {
		result = append(result, agent.SkillMatch{Score: h.score, Name: h.m.Name, Description: h.m.Description, Path: h.m.Path, RequiredTools: []string{"read_skill"}})
	}
	return result, nil
}

// ListSkillSummaries exposes current metadata without reading skill bodies.
func (s *Service) ListSkillSummaries() ([]agent.SkillSummary, error) {
	if s == nil || !s.opts.Enabled || s.Directory == nil {
		return nil, nil
	}
	items, err := s.Directory.List()
	if err != nil {
		return nil, err
	}
	result := make([]agent.SkillSummary, 0, len(items))
	for _, m := range items {
		result = append(result, agent.SkillSummary{Name: m.Name, Description: m.Description, Path: m.Path, Triggers: m.Triggers, NonTriggers: m.NonTriggers})
	}
	return result, nil
}

func skillPath(name, file string) (string, error) {
	if !directoryName.MatchString(name) {
		return "", errors.New("invalid skill name")
	}
	if file == "" {
		file = "SKILL.md"
	}
	if strings.ContainsAny(file, "\\:") || !filepath.IsLocal(file) || path.Clean(file) != file || file == "." {
		return "", errors.New("file must be a relative path within the skill")
	}
	return file, nil
}

func (d *Directory) Read(name, file string) (string, error) {
	file, err := skillPath(name, file)
	if err != nil {
		return "", err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return "", err
	}
	defer root.Close()
	sub, err := root.OpenRoot(name)
	if err != nil {
		return "", err
	}
	defer sub.Close()
	f, err := sub.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxSkillFile+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxSkillFile || !utf8.Valid(b) {
		return "", errors.New("skill file must be UTF-8 text, at most 5 MiB")
	}
	return string(b), nil
}

// Save writes one file atomically. Explicit overwrite avoids accidental replacement.
func (d *Directory) Save(name, file, content string, overwrite bool) error {
	file, err := skillPath(name, file)
	if err != nil {
		return err
	}
	if len(content) == 0 || len(content) > maxSkillFile || !utf8.ValidString(content) {
		return errors.New("content must be UTF-8 text, 1 byte..5 MiB")
	}
	if file == "SKILL.md" {
		if _, err := readMetadata(strings.NewReader(content), name); err != nil {
			return err
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return err
	}
	defer root.Close()
	if file != "SKILL.md" {
		f, err := root.Open(name + "/SKILL.md")
		if err != nil {
			return errors.New("save SKILL.md before supporting files")
		}
		_, err = readMetadata(f, name)
		f.Close()
		if err != nil {
			return err
		}
	}
	if err := root.MkdirAll(name, 0700); err != nil {
		return err
	}
	sub, err := root.OpenRoot(name)
	if err != nil {
		return err
	}
	defer sub.Close()
	if _, err := sub.Lstat(file); err == nil && !overwrite {
		return errors.New("file already exists; read it first and set overwrite=true to replace")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := sub.MkdirAll(path.Dir(file), 0700); err != nil {
		return err
	}
	// The temporary file is created inside the confined root, never through an absolute path.
	tmp := file + ".tmp"
	f, err := sub.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer sub.Remove(tmp)
	_, writeErr := io.WriteString(f, content)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := sub.Rename(tmp, file); err != nil {
		return fmt.Errorf("save skill: %w", err)
	}
	return nil
}
