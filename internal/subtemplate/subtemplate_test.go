package subtemplate

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestRenderProducesValidPanelTemplate(t *testing.T) {
	out, err := Render("https://cab.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, placeholder) {
		t.Fatal("placeholder left in output")
	}
	// The panel parses the file with html/template and executes it with the
	// subscription view-model; mimic that to catch template syntax breakage.
	tpl, err := template.New("sub").Parse(out)
	if err != nil {
		t.Fatalf("panel would fail to parse template: %v", err)
	}
	var page bytes.Buffer
	data := map[string]any{"sId": "abc123xyz", "subTitle": "T & Co", "enabled": true, "links": []string{}, "emails": []string{}}
	if err := tpl.Execute(&page, data); err != nil {
		t.Fatalf("panel would fail to execute template: %v", err)
	}
	html := page.String()
	want := "https://cab.example.com/s/abc123xyz"
	for _, frag := range []string{`href="` + want + `"`, "T &amp; Co"} {
		if !strings.Contains(html, frag) {
			t.Errorf("rendered page lacks %q:\n%s", frag, html)
		}
	}
	if strings.Count(html, want) != 1 {
		t.Errorf("expected the cabinet link once, on the button; got %d", strings.Count(html, want))
	}
	// An instant redirect looks like a phishing chain to Safari's checks; the
	// visitor must tap the button instead.
	lower := strings.ToLower(html)
	for _, banned := range []string{`http-equiv="refresh"`, "location.replace", "location.href", "<script"} {
		if strings.Contains(lower, banned) {
			t.Errorf("page must not redirect by itself, found %q", banned)
		}
	}
}

func TestRenderFallbackTitle(t *testing.T) {
	out, _ := Render("http://127.0.0.1:8080")
	tpl := template.Must(template.New("sub").Parse(out))
	var page bytes.Buffer
	if err := tpl.Execute(&page, map[string]any{"sId": "x", "subTitle": ""}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page.String(), "<title>Кабинет подписки</title>") {
		t.Errorf("fallback title missing:\n%s", page.String())
	}
}

func TestRenderRejectsBadInput(t *testing.T) {
	for _, in := range []string{"", "   ", "https://x.test/\"><script>"} {
		if _, err := Render(in); err == nil {
			t.Errorf("Render(%q) should fail", in)
		}
	}
}
