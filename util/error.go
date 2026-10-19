package util

import (
	"errors"
	"strings"
)

// ErrorTree shows the chain of wrapped errors one layer per line, each layer with only
// what it adds: what every step was doing, down to the cause. Joined errors are branches.
func ErrorTree(err error) string {
	var sb strings.Builder
	writeErrorTree(&sb, err, "", true)
	return strings.TrimRight(sb.String(), "\n")
}

func writeErrorTree(sb *strings.Builder, err error, indent string, root bool) {
	for err != nil {
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			for _, e := range joined.Unwrap() {
				writeErrorTree(sb, e, indent, root)
			}
			return
		}

		own := err.Error()
		next := errors.Unwrap(err)
		if next != nil {
			own = strings.TrimSpace(strings.TrimSuffix(own, next.Error()))
			own = strings.TrimSpace(strings.TrimSuffix(own, ":"))
		}
		// wrappers that add nothing (e.g. "%w") don't get a line
		if own != "" {
			if root {
				sb.WriteString(own)
				sb.WriteString("\n")
				root = false
			} else {
				sb.WriteString(indent)
				sb.WriteString("└ ")
				sb.WriteString(own)
				sb.WriteString("\n")
				indent += "  "
			}
		}
		err = next
	}
}
