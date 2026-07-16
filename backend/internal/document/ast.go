package document

import (
	"fmt"
	"time"
)

const (
	BlockTypeHeading       = "heading"
	BlockTypeParagraph     = "paragraph"
	BlockTypeOrderedList   = "ordered_list"
	BlockTypeUnorderedList = "unordered_list"
	BlockTypeListItem      = "list_item"
	BlockTypeTable         = "table"
	BlockTypeTableRow      = "table_row"
	BlockTypeTableCell     = "table_cell"
	BlockTypeCode          = "code_block"
	BlockTypeCommand       = "command_block"
	BlockTypeQuote         = "quote"
	BlockTypeImage         = "image"
	BlockTypeImageCaption  = "image_caption"
	BlockTypeWarning       = "warning"
	BlockTypeNote          = "note"
	BlockTypePageBreak     = "page_break"
	BlockTypeHeader        = "header"
	BlockTypeFooter        = "footer"
	BlockTypeUnknown       = "unknown"
)

type DocumentAST struct {
	DocumentID    int64            `json:"documentId"`
	Title         string           `json:"title"`
	Language      string           `json:"language"`
	Metadata      DocumentMetadata `json:"metadata"`
	Blocks        []DocumentBlock  `json:"blocks"`
	ParseWarnings []ParseWarning   `json:"parseWarnings"`
	ParserName    string           `json:"parserName"`
	ParserVersion string           `json:"parserVersion"`
}

type DocumentBlock struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Level       int             `json:"level"`
	Text        string          `json:"text"`
	Page        *int            `json:"page,omitempty"`
	SectionPath []string        `json:"sectionPath,omitempty"`
	Order       int             `json:"order"`
	Attributes  map[string]any  `json:"attributes,omitempty"`
	Children    []DocumentBlock `json:"children,omitempty"`
}

type DocumentMetadata struct {
	Author           string     `json:"author"`
	Subject          string     `json:"subject"`
	Keywords         []string   `json:"keywords,omitempty"`
	CreatedAt        *time.Time `json:"createdAt,omitempty"`
	ModifiedAt       *time.Time `json:"modifiedAt,omitempty"`
	DeclaredVersion  string     `json:"declaredVersion"`
	DeclaredOwner    string     `json:"declaredOwner"`
	DeclaredReviewer string     `json:"declaredReviewer"`
}

type ParseWarning struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Page    *int           `json:"page,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

func newBlock(blockType, text string, level, order int) DocumentBlock {
	return DocumentBlock{
		ID:         blockID(order),
		Type:       blockType,
		Level:      level,
		Text:       text,
		Order:      order,
		Attributes: map[string]any{},
	}
}

func blockID(order int) string {
	return fmt.Sprintf("block-%06d", order)
}
