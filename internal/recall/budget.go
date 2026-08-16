package recall

// Overflow is appended when Budget drops lines.
const Overflow = "… use search for more"

// Budget keeps the prefix of lines that fits in maxLines and maxBytes
// (whichever limit hits first). If anything is dropped, Overflow is appended.
func Budget(lines []string, maxLines int, maxBytes int) []string {
	out := make([]string, 0, len(lines))
	nBytes := 0
	truncated := false
	for i, line := range lines {
		if maxLines >= 0 && i >= maxLines {
			truncated = true
			break
		}
		if nBytes+len(line) > maxBytes {
			truncated = true
			break
		}
		out = append(out, line)
		nBytes += len(line)
	}
	if truncated {
		out = append(out, Overflow)
	}
	return out
}
