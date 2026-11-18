package output

import "strings"

// Sanitize makes server-provided text safe to print to a terminal. C0 control
// characters (except tab and newline) and DEL are replaced with caret
// notation ("^[" for ESC), and C1 control characters with U+FFFD, so a
// repository description or issue title cannot inject escape sequences
// (OSC 52 clipboard writes, OSC 8 links, screen clearing, cursor movement).
// gh does the same with its asciisanitizer.
func Sanitize(s string) string {
	if !needsSanitize(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n':
			b.WriteRune(r)
		case r < 0x20:
			b.WriteByte('^')
			b.WriteRune(r + 0x40)
		case r == 0x7f:
			b.WriteString("^?")
		case r >= 0x80 && r <= 0x9f:
			b.WriteRune('�')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func needsSanitize(s string) bool {
	for _, r := range s {
		if (r < 0x20 && r != '\t' && r != '\n') || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return true
		}
	}
	return false
}
