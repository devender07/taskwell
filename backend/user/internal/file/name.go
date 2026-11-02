package file

import (
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxNameLen = 200

var (
	imageTypes  = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}
	defaultName = "file"
)

// cleanName keeps only the last path part of a client's file name, drops invalid UTF-8 and control and format
// characters (such as right-to-left overrides), and caps it at maxNameLen characters.
func cleanName(s string) string {
	s = strings.ToValidUTF8(s, "")
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		s = s[i+1:]
	}
	s = strings.Map(func(r rune) rune {
		if unicode.In(r, unicode.Cc, unicode.Cf) || r == utf8.RuneError {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxNameLen {
		s = strings.TrimSpace(string(r[:maxNameLen]))
	}
	if s == "" || s == "." || s == ".." {
		return defaultName
	}
	return s
}

// fileURL is where GET /api/files serves the file; the name part is for people and download tools, not lookup.
func fileURL(id, name string) string { return "/api/files/" + id + "/" + url.PathEscape(name) }

// disposition is the Content-Disposition for serving a file: images inline, everything else as a download.
func disposition(contentType, name string) string {
	if isImage(contentType) {
		return "inline"
	}
	return "attachment; filename*=UTF-8''" + strings.ReplaceAll(url.QueryEscape(name), "+", "%20")
}

func isImage(contentType string) bool { return slices.Contains(imageTypes, contentType) }
