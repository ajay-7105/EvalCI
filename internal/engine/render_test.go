package engine

import (
	"strings"
	"testing"
)

func TestTemplateRender(t *testing.T) {
	tests := []struct {
		name       string
		prompt     string
		input      string
		wantSystem string
		wantUser   string
		wantErr    string
	}{
		{"no placeholder is system plus user", "You are helpful.", "hi", "You are helpful.", "hi", ""},
		{"input placeholder is single user message", "Answer: {{input}} thanks", "hi", "", "Answer: hi thanks", ""},
		{"placeholder with spaces", "A {{ input }} B", "x", "", "A x B", ""},
		{"multiple placeholders", "{{input}}-{{input}}", "x", "", "x-x", ""},
		{"escaped braces are literal", `Use \{{name}} syntax`, "hi", "Use {{name}} syntax", "hi", ""},
		{"escaped input is literal", `\{{input}} and {{input}}`, "Z", "", "{{input}} and Z", ""},
		{"json example escaped", `Reply as \{{"a": 1}}`, "q", `Reply as {{"a": 1}}`, "q", ""},
		{"single pass does not re-expand input", "X {{input}}", "{{input}}", "", "X {{input}}", ""},
		{"input containing unknown placeholder is fine", "X {{input}}", "{{other}}", "", "X {{other}}", ""},
		{"unknown placeholder rejected", "line1\nHello {{name}}", "", "", "", "p.txt:2: unknown placeholder {{name}}"},
		{"misspelled input rejected", "{{imput}}", "", "", "", "p.txt:1: unknown placeholder {{imput}}"},
		{"unterminated rejected", "ok\nok\n{{input", "", "", "", "p.txt:3: unterminated"},
		{"empty prompt", "", "hi", "", "hi", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tpl, err := ParseTemplate("p.txt", tt.prompt)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			sys, user := tpl.Render(tt.input)
			if sys != tt.wantSystem || user != tt.wantUser {
				t.Fatalf("Render = (%q, %q), want (%q, %q)", sys, user, tt.wantSystem, tt.wantUser)
			}
		})
	}
}

func TestLoadTemplateMissingFile(t *testing.T) {
	if _, err := LoadTemplate("/nonexistent/prompt.txt"); err == nil {
		t.Fatal("missing file must error")
	}
}
