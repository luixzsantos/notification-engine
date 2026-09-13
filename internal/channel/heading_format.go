package channel

import "strings"

// messageLine é uma linha da mensagem já normalizada por
// domain.ParseRichMessage, com o nível de título (1-3) detectado a partir de
// um marcador Markdown "#"/"##"/"###" — Level 0 significa texto comum.
type messageLine struct {
	Level int
	Text  string
}

// headingLines divide a mensagem em linhas marcadas com seu nível de
// título, para que cada canal decida como renderizar (nativamente, em
// negrito, ou como um elemento de título de verdade). O Discord já entende
// "#"/"##"/"###" nativamente, então não precisa desse helper.
func headingLines(message string) []messageLine {
	if message == "" {
		return nil
	}

	lines := strings.Split(message, "\n")
	result := make([]messageLine, len(lines))
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "### "):
			result[i] = messageLine{Level: 3, Text: strings.TrimPrefix(line, "### ")}
		case strings.HasPrefix(line, "## "):
			result[i] = messageLine{Level: 2, Text: strings.TrimPrefix(line, "## ")}
		case strings.HasPrefix(line, "# "):
			result[i] = messageLine{Level: 1, Text: strings.TrimPrefix(line, "# ")}
		default:
			result[i] = messageLine{Level: 0, Text: line}
		}
	}
	return result
}

// hasHeading reporta se alguma linha tem nível de título.
func hasHeading(lines []messageLine) bool {
	for _, l := range lines {
		if l.Level > 0 {
			return true
		}
	}
	return false
}
