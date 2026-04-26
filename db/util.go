package db

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

var nonAlpha = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.ToLower(s)
	s = nonAlpha.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 32 {
		s = s[:32]
	}
	return s
}

func shortID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}
