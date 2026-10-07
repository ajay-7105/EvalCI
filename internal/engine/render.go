package engine

import (
	"fmt"
	"os"
	"strings"
)

// Template is a parsed prompt file. Without a placeholder the whole text is the system
// message and the case input is the user message; with {{input}} the input is substituted
// and everything is sent as one user message.
type Template struct {
	parts    []string // literal text segments; len(parts) == placeholders+1
	hasInput bool
}

// LoadTemplate reads and parses the prompt file at path.
func LoadTemplate(path string) (*Template, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading prompt file: %w", err)
	}
	t, err := ParseTemplate(path, string(data))
	if err != nil {
		return nil, err
	}
	return t, nil
}

// ParseTemplate parses prompt text. name is used in error messages. `\{{` renders a literal
// `{{`; any placeholder other than {{input}} is an error naming the file and line.
func ParseTemplate(name, text string) (*Template, error) {
	t := &Template{}
	var cur strings.Builder
	for i := 0; i < len(text); {
		switch {
		case strings.HasPrefix(text[i:], `\{{`):
			cur.WriteString("{{")
			i += 3
		case strings.HasPrefix(text[i:], "{{"):
			end := strings.Index(text[i:], "}}")
			if end < 0 {
				return nil, fmt.Errorf("%s:%d: unterminated placeholder (write \\{{ for a literal)", name, lineOf(text, i))
			}
			ph := strings.TrimSpace(text[i+2 : i+end])
			if ph != "input" {
				return nil, fmt.Errorf("%s:%d: unknown placeholder {{%s}} (only {{input}} is supported; write \\{{ for a literal)", name, lineOf(text, i), ph)
			}
			t.parts = append(t.parts, cur.String())
			cur.Reset()
			t.hasInput = true
			i += end + 2
		default:
			cur.WriteByte(text[i])
			i++
		}
	}
	t.parts = append(t.parts, cur.String())
	return t, nil
}

func lineOf(text string, offset int) int {
	return strings.Count(text[:offset], "\n") + 1
}

// Render builds the system and user messages for one case input. Substitution is single
// pass: the input is never re-scanned for placeholders.
func (t *Template) Render(input string) (system, user string) {
	if !t.hasInput {
		return t.parts[0], input
	}
	return "", strings.Join(t.parts, input)
}
