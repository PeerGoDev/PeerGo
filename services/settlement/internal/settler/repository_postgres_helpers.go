package settler

import "regexp"

var errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func validErrorCode(value string) bool {
	return errorCodePattern.MatchString(value)
}
