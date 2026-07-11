package document

import (
	"strings"
	"unicode/utf8"

	"aiops-platform/backend/internal/model"
)

type textBlock struct {
	Content string
	Section string
}

func BuildChunks(document *model.KBDocument, content string, chunkSize, chunkOverlap int) []model.KBChunk {
	blocks := parseBlocks(content)
	chunks := make([]model.KBChunk, 0)
	var current strings.Builder
	currentSection := ""

	flush := func() {
		value := strings.TrimSpace(current.String())
		if value == "" {
			current.Reset()
			return
		}
		chunks = append(chunks, newChunk(document, len(chunks), value, currentSection))
		current.Reset()
	}

	for _, block := range blocks {
		if block.Content == "" {
			continue
		}
		blockRunes := []rune(block.Content)
		if len(blockRunes) > chunkSize {
			flush()
			for start := 0; start < len(blockRunes); {
				end := start + chunkSize
				if end > len(blockRunes) {
					end = len(blockRunes)
				}
				part := strings.TrimSpace(string(blockRunes[start:end]))
				if part != "" {
					chunks = append(chunks, newChunk(document, len(chunks), part, block.Section))
				}
				if end == len(blockRunes) {
					break
				}
				start = end
			}
			continue
		}

		nextSize := utf8.RuneCountInString(strings.TrimSpace(current.String())) + len(blockRunes)
		if current.Len() > 0 {
			nextSize += 2
		}
		if current.Len() > 0 && nextSize > chunkSize {
			flush()
		}
		if current.Len() > 0 && block.Section != currentSection {
			flush()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		if current.Len() == 0 {
			currentSection = block.Section
		}
		current.WriteString(block.Content)
	}
	flush()

	if chunkOverlap <= 0 {
		return chunks
	}
	for index := 1; index < len(chunks); index++ {
		overlap := tailRunes(chunks[index-1].Content, chunkOverlap)
		if overlap != "" && !strings.HasPrefix(chunks[index].Content, overlap) {
			chunks[index].Content = strings.TrimSpace(overlap + "\n\n" + chunks[index].Content)
			chunks[index].TokenCount = utf8.RuneCountInString(chunks[index].Content)
		}
	}
	for index := range chunks {
		chunks[index].ChunkIndex = index
	}
	return chunks
}

func parseBlocks(content string) []textBlock {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	section := ""
	var paragraph strings.Builder
	blocks := make([]textBlock, 0)

	flush := func() {
		value := strings.TrimSpace(paragraph.String())
		if value != "" {
			blocks = append(blocks, textBlock{Content: value, Section: section})
		}
		paragraph.Reset()
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if heading, ok := markdownHeading(trimmed); ok {
			flush()
			section = heading
			blocks = append(blocks, textBlock{Content: trimmed, Section: section})
			continue
		}
		if trimmed == "" {
			flush()
			continue
		}
		if paragraph.Len() > 0 {
			paragraph.WriteString("\n")
		}
		paragraph.WriteString(trimmed)
	}
	flush()
	return blocks
}

func markdownHeading(line string) (string, bool) {
	if !strings.HasPrefix(line, "#") {
		return "", false
	}
	count := 0
	for _, r := range line {
		if r != '#' {
			break
		}
		count++
	}
	if count == 0 || count > 6 || len(line) <= count || line[count] != ' ' {
		return "", false
	}
	heading := strings.TrimSpace(line[count:])
	return heading, heading != ""
}

func newChunk(document *model.KBDocument, index int, content, section string) model.KBChunk {
	var sourceTitle *string
	if document != nil && document.Title != "" {
		title := document.Title
		sourceTitle = &title
	}
	var sourceSection *string
	if strings.TrimSpace(section) != "" {
		value := strings.TrimSpace(section)
		sourceSection = &value
	}
	var documentID int64
	if document != nil {
		documentID = document.ID
	}
	return model.KBChunk{
		DocumentID:    documentID,
		ChunkIndex:    index,
		Content:       strings.TrimSpace(content),
		SourceTitle:   sourceTitle,
		SourceSection: sourceSection,
		TokenCount:    utf8.RuneCountInString(strings.TrimSpace(content)),
	}
}

func tailRunes(value string, count int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= count {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[len(runes)-count:]))
}
