package util

import (
	"regexp"
	"strings"
)

var functionNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_.:-]`)

func SanitizeFunctionName(name string) string {
	if name == "" {
		return ""
	}
	sanitized := functionNameSanitizer.ReplaceAllString(name, "_")
	if sanitized == "" {
		return "_"
	}
	first := sanitized[0]
	if (first < 'a' || first > 'z') && (first < 'A' || first > 'Z') && first != '_' {
		sanitized = "_" + sanitized
	}
	if len(sanitized) > 64 {
		sanitized = sanitized[:64]
	}
	return strings.TrimRight(sanitized, "")
}
