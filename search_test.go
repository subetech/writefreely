package writefreely

import (
	"strings"
	"sync"
	"testing"
)

func TestSearchTerms(t *testing.T) {
	got := searchTerms("  Рита   МОЙКОЙ рита  ")
	if strings.Join(got, "|") != "рита|мойкой" {
		t.Errorf("searchTerms: got %q", got)
	}
	if len(searchTerms(strings.Repeat("a b c d e f g h i j ", 3))) != searchMaxTerms {
		t.Errorf("searchTerms: expected at most %d terms", searchMaxTerms)
	}
	if len(searchTerms("   ")) != 0 {
		t.Error("searchTerms: expected no terms for blank query")
	}
}

func TestEscapeLike(t *testing.T) {
	if got := escapeLike("100%_done!"); got != "100!%!_done!!" {
		t.Errorf("escapeLike: got %q", got)
	}
}

func TestSearchSnippet(t *testing.T) {
	text := strings.Repeat("слово ", 30) + "Рита <b>пришла</b> над Мойкой. " + strings.Repeat("ещё ", 80)
	got := string(searchSnippet(text, []string{"рита", "мойкой"}))
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("searchSnippet: expected ellipses around excerpt, got %q", got)
	}
	if !strings.Contains(got, "<mark>Рита</mark>") || !strings.Contains(got, "<mark>Мойкой</mark>") {
		t.Errorf("searchSnippet: expected highlighted terms, got %q", got)
	}
	if strings.Contains(got, "<b>") {
		t.Errorf("searchSnippet: expected HTML to be escaped, got %q", got)
	}

	short := string(searchSnippet("Hello world", []string{"world"}))
	if short != "Hello <mark>world</mark>" {
		t.Errorf("searchSnippet: got %q", short)
	}
}

func TestPreviewLines(t *testing.T) {
	p := &Post{Content: "First **paragraph**.\n\n![img](/media/x.png)\n\nSecond <i>one</i>.\n\nThird.\n\nFourth."}
	if got := p.PreviewLines(3); got != "First paragraph.\nSecond one.\nThird." {
		t.Errorf("PreviewLines: got %q", got)
	}
}

func TestNormalizeGridLines(t *testing.T) {
	for in, want := range map[int]int{0: collGridLinesDefault, -1: collGridLinesDefault, 1: 1, 7: 7, 100: collGridLinesMax} {
		if got := normalizeGridLines(in); got != want {
			t.Errorf("normalizeGridLines(%d): got %d, want %d", in, got, want)
		}
	}
}

func TestMediaFilenameReg(t *testing.T) {
	for name, ok := range map[string]bool{
		"AbC123.png":       true,
		"x.webp":           true,
		"../config.ini":    false,
		".upload-123":      false,
		"a.svg":            false,
		"a.png.php":        false,
		"a/b.png":          false,
		"":                 false,
		"abc.PNG":          false,
		"abc def.png":      false,
		"abc.png\x00.html": false,
	} {
		if mediaFilenameReg.MatchString(name) != ok {
			t.Errorf("mediaFilenameReg(%q): expected %v", name, ok)
		}
	}
}

func TestHighlightTerms(t *testing.T) {
	got := string(highlightTerms("Ночью в зал не входить. Глава 2", []string{"зал", "глава"}, false, 0))
	if got != "Ночью в <mark>зал</mark> не входить. <mark>Глава</mark> 2" {
		t.Errorf("highlightTerms: got %q", got)
	}
	if !containsAllTerms("Ночью в зал не входить", []string{"ночью", "зал"}) || containsAllTerms("Ночью в зал", []string{"ночью", "мост"}) {
		t.Error("containsAllTerms: wrong result")
	}
}

func TestAssetURL(t *testing.T) {
	assetsStaticDir = "static"
	assetVersions = &sync.Map{}
	u := assetURL("/js/h.js")
	if !strings.HasPrefix(u, "/js/h.js?v=") || len(u) != len("/js/h.js?v=")+10 {
		t.Errorf("assetURL: got %q", u)
	}
	if assetURL("/js/h.js") != u {
		t.Error("assetURL: expected a stable version")
	}
	for _, p := range []string{"/js/nonexistent.js", "/js/../../config.ini"} {
		if got := assetURL(p); got != p {
			t.Errorf("assetURL(%q): got %q", p, got)
		}
	}
}
