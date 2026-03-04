package rag

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var reHeadingSlug = regexp.MustCompile(`[^a-z0-9]+`)

// MarkdownChunker splits markdown documents into chunks at heading boundaries.
type MarkdownChunker struct {
	MaxLinesPerSection int
}

func NewMarkdownChunker(maxLines int) *MarkdownChunker {
	if maxLines <= 0 {
		maxLines = 80
	}
	return &MarkdownChunker{MaxLinesPerSection: maxLines}
}

func (c *MarkdownChunker) ChunkFile(path string, src string) []Chunk {
	clean := cleanPath(path)
	lines := strings.Split(src, "\n")

	// Parse and skip YAML frontmatter.
	fmTags, fmTopic, fmEnd := parseFrontmatter(lines)

	contentLines := lines[fmEnd:]

	// Split into H2 sections.
	sections := splitAtHeadings(contentLines, "## ", fmEnd)

	// If no H2 headings found, emit the whole file as a single chunk.
	if len(sections) == 0 {
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		ch := Chunk{
			File:      clean,
			StartLine: 1,
			EndLine:   len(lines),
			Name:      stem,
			Type:      ChunkTypeDoc,
			Skills:    docSkills(fmTags),
			Code:      src,
		}
		ch.Description = extractMDDescription(contentLines, fmTopic)
		ch.ID = fmt.Sprintf("%s#%d-%d:%s", clean, 1, len(lines), headingSlug(stem))
		ch.Frontmatter = BuildFrontmatter(ch)
		return []Chunk{ch}
	}

	var chunks []Chunk
	for _, sec := range sections {
		// If section is too long and has H3 subsections, split deeper.
		if len(sec.lines) > c.MaxLinesPerSection && hasSubHeadings(sec.lines, "### ") {
			subs := splitAtHeadings(sec.lines, "### ", sec.startOffset)
			for _, sub := range subs {
				chunks = append(chunks, c.buildChunk(clean, sub, fmTags, fmTopic))
			}
		} else {
			chunks = append(chunks, c.buildChunk(clean, sec, fmTags, fmTopic))
		}
	}
	return chunks
}

type mdSection struct {
	heading     string
	lines       []string
	startOffset int // 0-based offset from start of file
}

func (c *MarkdownChunker) buildChunk(path string, sec mdSection, fmTags []string, fmTopic string) Chunk {
	startLine := sec.startOffset + 1 // 1-based
	endLine := sec.startOffset + len(sec.lines)
	code := strings.Join(sec.lines, "\n")
	slug := headingSlug(sec.heading)

	ch := Chunk{
		ID:        fmt.Sprintf("%s#%d-%d:%s", path, startLine, endLine, slug),
		File:      path,
		StartLine: startLine,
		EndLine:   endLine,
		Name:      sec.heading,
		Type:      ChunkTypeDoc,
		Skills:    docSkills(fmTags),
		Code:      code,
	}
	ch.Description = extractMDDescription(sec.lines, fmTopic)
	ch.Frontmatter = BuildFrontmatter(ch)
	return ch
}

// parseFrontmatter extracts tags and topic from YAML frontmatter.
// Returns (tags, topic, endLine) where endLine is the first line after the frontmatter block.
func parseFrontmatter(lines []string) (tags []string, topic string, endLine int) {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, "", 0
	}
	for i := 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "---" {
			return tags, topic, i + 1
		}
		key, val := splitFMLine(trimmed)
		switch key {
		case "tags":
			tags = parseFMList(val)
		case "topic":
			topic = strings.Trim(val, "\"'")
		}
	}
	// Unclosed frontmatter — treat as no frontmatter.
	return nil, "", 0
}

func splitFMLine(line string) (string, string) {
	idx := strings.Index(line, ": ")
	if idx < 0 {
		// Handle "key:value" without space
		idx = strings.Index(line, ":")
		if idx < 0 {
			return "", ""
		}
		return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:])
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+2:])
}

// parseFMList parses "[tag1, tag2, tag3]" format.
func parseFMList(val string) []string {
	val = strings.TrimSpace(val)
	val = strings.TrimPrefix(val, "[")
	val = strings.TrimSuffix(val, "]")
	parts := strings.Split(val, ",")
	var out []string
	for _, p := range parts {
		s := strings.TrimSpace(p)
		s = strings.Trim(s, "\"'")
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// splitAtHeadings splits lines at the given heading prefix (e.g. "## " or "### ").
// startOffset is the 0-based line offset of lines[0] within the file.
func splitAtHeadings(lines []string, prefix string, startOffset int) []mdSection {
	var sections []mdSection
	var current *mdSection

	for i, line := range lines {
		if strings.HasPrefix(line, prefix) && !strings.HasPrefix(line, prefix+"#") {
			if current != nil {
				sections = append(sections, *current)
			}
			heading := strings.TrimSpace(strings.TrimPrefix(line, strings.TrimRight(prefix, " ")))
			current = &mdSection{
				heading:     heading,
				lines:       []string{line},
				startOffset: startOffset + i,
			}
		} else if current != nil {
			current.lines = append(current.lines, line)
		}
		// Lines before first heading are skipped (preamble/title).
	}
	if current != nil {
		sections = append(sections, *current)
	}
	return sections
}

func hasSubHeadings(lines []string, prefix string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) && !strings.HasPrefix(line, prefix+"#") {
			return true
		}
	}
	return false
}

// extractMDDescription gets the first non-empty paragraph from section lines.
// If fmTopic is set, it's prepended.
func extractMDDescription(lines []string, fmTopic string) string {
	var para []string
	inPara := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip heading lines.
		if strings.HasPrefix(trimmed, "#") {
			if inPara {
				break
			}
			continue
		}
		if trimmed == "" {
			if inPara {
				break
			}
			continue
		}
		// Skip frontmatter delimiters.
		if trimmed == "---" {
			continue
		}
		para = append(para, trimmed)
		inPara = true
	}
	desc := strings.Join(para, " ")
	if len(desc) > 300 {
		desc = desc[:300]
	}
	if fmTopic != "" && desc != "" {
		return fmTopic + " — " + desc
	}
	if fmTopic != "" {
		return fmTopic
	}
	if desc != "" {
		return desc
	}
	return "Markdown documentation section"
}

func docSkills(fmTags []string) []string {
	skills := []string{"doc", "markdown"}
	for _, t := range fmTags {
		// Avoid duplicates with base skills.
		if t != "doc" && t != "markdown" {
			skills = append(skills, t)
		}
	}
	return skills
}

func headingSlug(heading string) string {
	s := strings.ToLower(strings.TrimSpace(heading))
	s = reHeadingSlug.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "section"
	}
	return s
}
