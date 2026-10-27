package skill

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
)

// MonitorPreview is a read-only adapter for the admin file viewer. It is not
// registered as an Agent tool and never executes or modifies skill resources.
func (d *Directory) MonitorPreview(name, file string) (map[string]any, error) {
	file, err := skillPath(name, file)
	if err != nil {
		return nil, err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	root, err := d.openProject(name)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	data, err := readProjectFile(root, file, maxSkillFile)
	if err != nil {
		return nil, fmt.Errorf("无法预览文件（仅支持不超过 5 MiB 的普通文件）: %w", err)
	}
	mime := http.DetectContentType(data)
	result := map[string]any{"path": file, "size": len(data)}
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/x-icon", "image/bmp":
		result["kind"], result["mime"], result["data"] = "image", mime, base64.StdEncoding.EncodeToString(data)
	default:
		if !utf8.Valid(data) || strings.ContainsRune(string(data), '\x00') {
			result["kind"] = "unsupported"
		} else {
			result["kind"], result["content"] = "text", string(data)
		}
	}
	return result, nil
}
