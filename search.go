/*
 * Copyright © 2026 Musing Studio LLC.
 *
 * This file is part of WriteFreely.
 *
 * WriteFreely is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License, included
 * in the LICENSE file in this source code package.
 */

package writefreely

import (
	"database/sql"
	"html"
	"html/template"
	"net/http"
	"strings"
	"unicode/utf8"

	stripmd "github.com/writeas/go-strip-markdown/v2"
	"github.com/writeas/impart"
	"github.com/writeas/web-core/log"
	"github.com/writefreely/writefreely/config"
)

const (
	// searchMaxResults caps the number of posts returned for a search query.
	searchMaxResults = 100
	// searchMaxTerms caps the number of words taken from a search query.
	searchMaxTerms = 8
	// searchMaxQueryLen caps the length (in runes) of a search query.
	searchMaxQueryLen = 200

	searchSnippetBefore = 60
	searchSnippetLen    = 220
)

// SearchResult is a post matching a search query, with its title (matching
// terms highlighted) and a plain-text snippet of the start of its content.
type SearchResult struct {
	PublicPost
	TitleHTML template.HTML
	Snippet   template.HTML
}

// searchTerms splits the given query into lowercase, de-duplicated terms.
func searchTerms(q string) []string {
	if utf8.RuneCountInString(q) > searchMaxQueryLen {
		q = string([]rune(q)[:searchMaxQueryLen])
	}
	seen := map[string]bool{}
	terms := []string{}
	for _, t := range strings.Fields(strings.ToLower(q)) {
		if seen[t] {
			continue
		}
		seen[t] = true
		terms = append(terms, t)
		if len(terms) == searchMaxTerms {
			break
		}
	}
	return terms
}

// escapeLike escapes LIKE wildcards in s, using '!' as the escape character.
func escapeLike(s string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(s)
}

// SearchPosts returns posts in the given collection whose title contains all
// of the words in the query (case-insensitively), newest first. Posts without
// an explicit title are matched by the title shown for them (derived from the
// start of their content). Scheduled (future) posts are only included when
// includeFuture is true.
func (db *datastore) SearchPosts(cfg *config.Config, c *Collection, q string, includeFuture bool) ([]SearchResult, error) {
	terms := searchTerms(q)
	if len(terms) == 0 {
		return []SearchResult{}, nil
	}

	lower := "LOWER"
	if db.driverName == driverSQLite {
		lower = "wf_lower"
	}
	titleMatch := ""
	params := []interface{}{c.ID}
	for i, t := range terms {
		if i > 0 {
			titleMatch += " AND "
		}
		titleMatch += lower + "(title) LIKE ? ESCAPE '!'"
		params = append(params, "%"+escapeLike(t)+"%")
	}
	// Untitled posts are fetched too, and matched against their display
	// title below.
	where := "collection_id = ? AND ((" + titleMatch + ") OR title = '' OR title IS NULL)"
	if !includeFuture {
		where += " AND created <= " + db.now()
	}

	rows, err := db.Query("SELECT "+postCols+" FROM posts WHERE "+where+" ORDER BY created DESC", params...)
	if err != nil {
		log.Error("Failed searching posts: %v", err)
		return nil, impart.HTTPError{Status: http.StatusInternalServerError, Message: "Couldn't search posts."}
	}
	defer rows.Close()

	results := []SearchResult{}
	for rows.Next() {
		p := &Post{}
		err = rows.Scan(&p.ID, &p.Slug, &p.Font, &p.Language, &p.RTL, &p.Privacy, &p.OwnerID, &p.CollectionID, &p.PinnedPosition, &p.Created, &p.Updated, &p.ViewCount, &p.Title, &p.Content)
		if err != nil {
			log.Error("Failed scanning row: %v", err)
			break
		}
		p.extractData()
		title := p.PlainDisplayTitle()
		if !containsAllTerms(title, terms) {
			continue
		}
		// Respect paid content: only show what this reader would see on the
		// blog index.
		p.handlePremiumContent(c, includeFuture, false, cfg)

		results = append(results, SearchResult{
			PublicPost: p.processPost(),
			TitleHTML:  highlightTerms(title, terms, false, 0),
			Snippet:    highlightTerms(postPlainText(p.Content), terms, false, searchSnippetLen),
		})
		if len(results) >= searchMaxResults {
			break
		}
	}
	if err = rows.Err(); err != nil {
		log.Error("Error after Next() on rows: %v", err)
	}
	return results, nil
}

// containsAllTerms returns whether s contains all of the given lowercase terms,
// case-insensitively.
func containsAllTerms(s string, terms []string) bool {
	s = strings.ToLower(s)
	for _, t := range terms {
		if !strings.Contains(s, t) {
			return false
		}
	}
	return true
}

// postPlainText strips HTML and Markdown from post content, keeping line breaks.
func postPlainText(content string) string {
	content = strings.Replace(content, shortCodePaid, "", -1)
	content = stripHTMLWithoutEscaping(content)
	return stripmd.StripOptions(content, stripmd.Options{SkipImages: true})
}

// searchSnippet extracts a short excerpt of text around the first match of any
// of the given (lowercase) terms, HTML-escaping it and wrapping matches in <mark>.
func searchSnippet(text string, terms []string) template.HTML {
	return highlightTerms(text, terms, true, searchSnippetLen)
}

// highlightTerms HTML-escapes the given text, wrapping any of the given
// (lowercase) terms in <mark>. If maxLen > 0, only an excerpt of up to maxLen
// runes is returned: starting near the first match if center is true, or at
// the beginning of the text otherwise.
func highlightTerms(text string, terms []string, center bool, maxLen int) template.HTML {
	text = strings.Join(strings.Fields(text), " ")
	orig := []rune(text)
	if maxLen <= 0 {
		maxLen = len(orig)
	}
	lower := []rune(strings.ToLower(text))
	if len(orig) != len(lower) {
		// Lowercasing changed the text's length, so rune offsets won't line
		// up; fall back to an un-highlighted excerpt.
		return template.HTML(html.EscapeString(truncateRunes(orig, 0, maxLen)))
	}

	termRunes := make([][]rune, 0, len(terms))
	for _, t := range terms {
		termRunes = append(termRunes, []rune(t))
	}

	start := 0
	if center {
		// Find where the first match is, to center the snippet around it
		first := -1
		for i := range lower {
			if matchAt(lower, i, termRunes) > 0 {
				first = i
				break
			}
		}
		if first > searchSnippetBefore {
			start = first - searchSnippetBefore
			// Don't cut words in half
			for start < first && orig[start] != ' ' {
				start++
			}
		}
	}
	end := start + maxLen
	if end > len(orig) {
		end = len(orig)
	}

	var sb strings.Builder
	if start > 0 {
		sb.WriteString("…")
	}
	for i := start; i < end; {
		if n := matchAt(lower, i, termRunes); n > 0 {
			if i+n > end {
				n = end - i
			}
			sb.WriteString("<mark>")
			sb.WriteString(html.EscapeString(string(orig[i : i+n])))
			sb.WriteString("</mark>")
			i += n
			continue
		}
		sb.WriteString(html.EscapeString(string(orig[i])))
		i++
	}
	if end < len(orig) {
		sb.WriteString("…")
	}
	return template.HTML(sb.String())
}

// matchAt returns the length of the longest term found at position i, or 0.
func matchAt(s []rune, i int, terms [][]rune) int {
	best := 0
	for _, t := range terms {
		if len(t) <= best || i+len(t) > len(s) {
			continue
		}
		match := true
		for j := range t {
			if s[i+j] != t[j] {
				match = false
				break
			}
		}
		if match {
			best = len(t)
		}
	}
	return best
}

func truncateRunes(r []rune, start, l int) string {
	end := start + l
	if end >= len(r) {
		return string(r[start:])
	}
	return string(r[start:end]) + "…"
}

// PreviewLines returns up to n of the first non-empty lines of the post's
// content as plain text, for showing on cards in the grid layout.
func (p *Post) PreviewLines(n int) string {
	const maxLineLen = 400
	lines := []string{}
	for _, l := range strings.Split(postPlainText(p.Content), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if utf8.RuneCountInString(l) > maxLineLen {
			l = truncateRunes([]rune(l), 0, maxLineLen)
		}
		lines = append(lines, l)
		if len(lines) >= n {
			break
		}
	}
	return strings.Join(lines, "\n")
}

// GetCollectionLanguage returns the language of the given collection, based on
// its most recent post, for localizing the blog's UI.
func (db *datastore) GetCollectionLanguage(c *Collection) string {
	if c.Language != "" {
		return c.Language
	}
	var lang sql.NullString
	err := db.QueryRow("SELECT language FROM posts WHERE collection_id = ? AND language IS NOT NULL AND language != '' ORDER BY created DESC LIMIT 1", c.ID).Scan(&lang)
	if err != nil && err != sql.ErrNoRows {
		log.Error("Unable to get collection language: %v", err)
	}
	return lang.String
}

// uiPhrases holds translations of reader-facing UI strings that aren't part of
// web-core's l10n package. English is the fallback.
var uiPhrases = map[string]map[string]string{
	"": {
		"search":             "Search",
		"search placeholder": "Search",
		"scheduled for":      "Scheduled for",
		"search results":     "Results for",
		"search no results":  "Nothing found for",
		"search clear":       "Back to all posts",
		"search limited":     "Showing the most recent matches only.",
	},
	"ru": {
		"search":             "Найти",
		"search placeholder": "Поиск",
		"scheduled for":      "Запланировано на",
		"search results":     "Результаты поиска:",
		"search no results":  "Ничего не найдено по запросу",
		"search clear":       "Все посты",
		"search limited":     "Показаны только самые свежие совпадения.",
	},
}

func uiStr(term, lang string) string {
	if s, ok := uiPhrases[lang][term]; ok {
		return s
	}
	return uiPhrases[""][term]
}

// GetScheduledPosts returns the user's blog posts with a publish date in the
// future, soonest first.
func (db *datastore) GetScheduledPosts(u *User) ([]PublicPost, error) {
	rows, err := db.Query(`SELECT p.id, p.slug, p.title, p.language, p.created, p.updated, p.content, c.id, c.alias, c.title
		FROM posts p INNER JOIN collections c ON p.collection_id = c.id
		WHERE p.owner_id = ? AND p.created > `+db.now()+`
		ORDER BY p.created ASC`, u.ID)
	if err != nil {
		log.Error("Failed selecting scheduled posts: %v", err)
		return nil, impart.HTTPError{Status: http.StatusInternalServerError, Message: "Couldn't retrieve scheduled posts."}
	}
	defer rows.Close()

	posts := []PublicPost{}
	for rows.Next() {
		p := &Post{}
		c := Collection{}
		err = rows.Scan(&p.ID, &p.Slug, &p.Title, &p.Language, &p.Created, &p.Updated, &p.Content, &c.ID, &c.Alias, &c.Title)
		if err != nil {
			log.Error("Failed scanning row: %v", err)
			break
		}
		p.extractData()
		pp := p.processPost()
		pp.Collection = &CollectionObj{Collection: c}
		posts = append(posts, pp)
	}
	if err = rows.Err(); err != nil {
		log.Error("Error after Next() on rows: %v", err)
	}
	return posts, nil
}
