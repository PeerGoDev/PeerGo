// Package jetstreamv1 contains literal-name validation shared by independent
// event contracts. It deliberately knows nothing about a particular stream's
// payload or retention policy.
package jetstreamv1

import (
	"strings"
	"unicode"
)

func ValidStreamName(value string) bool {
	if value == "" || len(value) > 255 {
		return false
	}
	for _, character := range value {
		if character == '.' || character == '*' || character == '>' || character == '/' || character == '\\' ||
			unicode.IsSpace(character) || !unicode.IsPrint(character) {
			return false
		}
	}
	return true
}

func ValidLiteralSubject(value string) bool {
	if value == "" || len(value) > 255 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, token := range strings.Split(value, ".") {
		if token == "" {
			return false
		}
		for _, character := range token {
			if character == '*' || character == '>' || unicode.IsSpace(character) || !unicode.IsPrint(character) {
				return false
			}
		}
	}
	return true
}
