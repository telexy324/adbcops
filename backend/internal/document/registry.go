package document

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultParseTimeout = 120 * time.Second
	defaultMaxBlocks    = 50000
	defaultMaxPages     = 1000
)

var (
	ErrParseTimeout         = errors.New("document parsing timed out")
	ErrBlockLimitExceeded   = errors.New("document block limit exceeded")
	ErrPageLimitExceeded    = errors.New("document page limit exceeded")
	ErrFileTypeMismatch     = errors.New("file content does not match extension")
	ErrLegacyDocUnsupported = errors.New("legacy .doc is unsupported; convert the file to .docx")
)

type ParseRequest struct {
	Path       string
	FileName   string
	FileType   string
	DocumentID int64
	Title      string
}

type Parser interface {
	Name() string
	Version() string
	FileTypes() []string
	Parse(ctx context.Context, request ParseRequest, limits ParseLimits) (*DocumentAST, error)
}

type ParseLimits struct {
	Timeout   time.Duration
	MaxBlocks int
	MaxPages  int
	MaxBytes  int64
}

func DefaultParseLimits() ParseLimits {
	return ParseLimits{Timeout: defaultParseTimeout, MaxBlocks: defaultMaxBlocks, MaxPages: defaultMaxPages, MaxBytes: maxExtractedTextBytes}
}

type ParserRegistry struct {
	mu      sync.RWMutex
	parsers map[string]Parser
	limits  ParseLimits
}

func NewParserRegistry(limits ParseLimits, parsers ...Parser) (*ParserRegistry, error) {
	limits = normalizeParseLimits(limits)
	registry := &ParserRegistry{parsers: make(map[string]Parser), limits: limits}
	for _, parser := range parsers {
		if err := registry.Register(parser); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func NewDefaultParserRegistry(limits ParseLimits) (*ParserRegistry, error) {
	return NewParserRegistry(limits, markdownParser{}, textParser{}, docxParser{}, xlsxParser{}, pdfParser{})
}

func (r *ParserRegistry) Register(parser Parser) error {
	if parser == nil || strings.TrimSpace(parser.Name()) == "" || strings.TrimSpace(parser.Version()) == "" {
		return fmt.Errorf("invalid parser")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, fileType := range parser.FileTypes() {
		key := normalizeFileType(fileType)
		if key == "" {
			return fmt.Errorf("parser %s has empty file type", parser.Name())
		}
		if _, exists := r.parsers[key]; exists {
			return fmt.Errorf("parser already registered for %s", key)
		}
		r.parsers[key] = parser
	}
	return nil
}

func (r *ParserRegistry) Parse(ctx context.Context, request ParseRequest) (*DocumentAST, error) {
	if r == nil || strings.TrimSpace(request.Path) == "" {
		return nil, ErrInvalidFile
	}
	fileType := normalizeFileType(request.FileType)
	if fileType == "doc" || strings.EqualFold(filepath.Ext(request.FileName), ".doc") {
		return nil, ErrLegacyDocUnsupported
	}
	r.mu.RLock()
	parser := r.parsers[fileType]
	r.mu.RUnlock()
	if parser == nil {
		return nil, ErrUnsupportedExt
	}
	if err := checkFileSecurity(request.Path, fileType, r.limits.MaxBytes); err != nil {
		return nil, err
	}
	parseCtx, cancel := context.WithTimeout(ctx, r.limits.Timeout)
	defer cancel()
	type parseResult struct {
		ast *DocumentAST
		err error
	}
	resultChannel := make(chan parseResult, 1)
	go func() {
		ast, err := parser.Parse(parseCtx, request, r.limits)
		resultChannel <- parseResult{ast: ast, err: err}
	}()
	var result parseResult
	select {
	case <-parseCtx.Done():
		if errors.Is(parseCtx.Err(), context.DeadlineExceeded) {
			return nil, ErrParseTimeout
		}
		return nil, parseCtx.Err()
	case result = <-resultChannel:
	}
	if result.err != nil {
		if errors.Is(result.err, context.DeadlineExceeded) {
			return nil, ErrParseTimeout
		}
		return nil, result.err
	}
	ast := result.ast
	if ast == nil {
		return nil, ErrInvalidFile
	}
	ast.DocumentID = request.DocumentID
	if ast.Title == "" {
		ast.Title = request.Title
	}
	ast.ParserName = parser.Name()
	ast.ParserVersion = parser.Version()
	nextOrder := 0
	assignBlockIdentity(ast.Blocks, &nextOrder)
	if countBlocks(ast.Blocks) > r.limits.MaxBlocks {
		return nil, ErrBlockLimitExceeded
	}
	return ast, nil
}

func assignBlockIdentity(blocks []DocumentBlock, nextOrder *int) {
	for index := range blocks {
		*nextOrder++
		blocks[index].Order = *nextOrder
		blocks[index].ID = blockID(*nextOrder)
		assignBlockIdentity(blocks[index].Children, nextOrder)
	}
}

func normalizeParseLimits(limits ParseLimits) ParseLimits {
	defaults := DefaultParseLimits()
	if limits.Timeout <= 0 {
		limits.Timeout = defaults.Timeout
	}
	if limits.MaxBlocks <= 0 {
		limits.MaxBlocks = defaults.MaxBlocks
	}
	if limits.MaxPages <= 0 {
		limits.MaxPages = defaults.MaxPages
	}
	if limits.MaxBytes <= 0 {
		limits.MaxBytes = defaults.MaxBytes
	}
	return limits
}

func normalizeFileType(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), ".")
}

func countBlocks(blocks []DocumentBlock) int {
	total := 0
	for _, block := range blocks {
		total += 1 + countBlocks(block.Children)
	}
	return total
}

func checkContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func checkFileSecurity(path, fileType string, maxBytes int64) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat document: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 {
		return ErrInvalidFile
	}
	if info.Size() > maxBytes {
		return ErrFileTooLarge
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open document: %w", err)
	}
	defer file.Close()
	header := make([]byte, 8)
	n, err := file.Read(header)
	if err != nil && n == 0 {
		return ErrInvalidFile
	}
	header = header[:n]
	switch fileType {
	case "docx", "xlsx":
		if len(header) < 4 || string(header[:4]) != "PK\x03\x04" {
			return ErrFileTypeMismatch
		}
		if err := checkOfficeArchive(path, fileType, maxBytes); err != nil {
			return err
		}
	case "pdf":
		if len(header) < 5 || string(header[:5]) != "%PDF-" {
			return ErrFileTypeMismatch
		}
	case "md", "txt":
		if strings.ContainsRune(string(header), '\x00') {
			return ErrFileTypeMismatch
		}
	}
	return nil
}

func checkOfficeArchive(path, fileType string, maxBytes int64) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return ErrFileTypeMismatch
	}
	defer archive.Close()
	requiredEntry := "word/document.xml"
	if fileType == "xlsx" {
		requiredEntry = "xl/workbook.xml"
	}
	foundRequired := false
	var expandedBytes uint64
	for _, entry := range archive.File {
		cleanName := filepath.Clean(entry.Name)
		if filepath.IsAbs(cleanName) || cleanName == ".." || strings.HasPrefix(cleanName, "../") {
			return ErrPathTraversal
		}
		if cleanName == requiredEntry {
			foundRequired = true
		}
		expandedBytes += entry.UncompressedSize64
		if expandedBytes > uint64(maxBytes) {
			return ErrFileTooLarge
		}
	}
	if !foundRequired {
		return ErrFileTypeMismatch
	}
	return nil
}
