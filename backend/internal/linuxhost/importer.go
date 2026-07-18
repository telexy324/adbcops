package linuxhost

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"aiops-platform/backend/internal/model"
	"github.com/xuri/excelize/v2"
)

const MaxLinuxImportRows = 5000

const (
	DuplicateSkip                    = "skip"
	DuplicateUpdateMetadata          = "update_metadata"
	DuplicateReplaceConnectionConfig = "replace_connection_config"
	DuplicateCreateAsDisabled        = "create_as_disabled"
)

var (
	ErrImportTooManyRows = errors.New("linux host import exceeds 5000 rows")
	ErrImportFormat      = errors.New("unsupported linux host import format")
	ErrImportExpired     = errors.New("linux host import preview expired")
	ErrImportHasErrors   = errors.New("linux host import preview contains errors")
)

type ImportHostService interface {
	ListHosts(context.Context, *model.AppUser) ([]HostView, error)
	ListCredentialGroups(context.Context, *model.AppUser) ([]CredentialGroupView, error)
	CreateHost(context.Context, *model.AppUser, HostInput) (*HostView, error)
	UpdateHost(context.Context, *model.AppUser, int64, HostUpdateInput) (*HostView, error)
}

type ImportFile struct {
	Name          string
	Reader        io.Reader
	ColumnMapping map[string]string // canonical field -> uploaded header
	Strategy      string
}

type ImportIssue struct {
	Row     int    `json:"row"`
	Field   string `json:"field,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ImportPreviewRow struct {
	Row                  int           `json:"row"`
	Name                 string        `json:"name"`
	Host                 string        `json:"host"`
	Port                 int           `json:"port"`
	Environment          string        `json:"environment,omitempty"`
	AuthType             string        `json:"authType"`
	CredentialGroupName  string        `json:"credentialGroupName,omitempty"`
	CredentialConfigured bool          `json:"credentialConfigured"`
	GroupNames           []string      `json:"groupNames,omitempty"`
	Tags                 []string      `json:"tags,omitempty"`
	Action               string        `json:"action"`
	ExistingHostID       *int64        `json:"existingHostId,omitempty"`
	Issues               []ImportIssue `json:"issues,omitempty"`
}

type ImportPreview struct {
	Token             string             `json:"token"`
	Strategy          string             `json:"strategy"`
	Total             int                `json:"total"`
	Valid             int                `json:"valid"`
	Invalid           int                `json:"invalid"`
	Duplicates        int                `json:"duplicates"`
	ExpiresAt         time.Time          `json:"expiresAt"`
	Rows              []ImportPreviewRow `json:"rows"`
	TransactionPolicy string             `json:"transactionPolicy"`
}

type ImportResultItem struct {
	Row     int    `json:"row"`
	Status  string `json:"status"`
	HostID  int64  `json:"hostId,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type ImportResult struct {
	Total             int                `json:"total"`
	Created           int                `json:"created"`
	Updated           int                `json:"updated"`
	Skipped           int                `json:"skipped"`
	Failed            int                `json:"failed"`
	Items             []ImportResultItem `json:"items"`
	TransactionPolicy string             `json:"transactionPolicy"`
}

type importRow struct {
	preview    ImportPreviewRow
	input      HostInput
	existingID int64
}

type storedPreview struct {
	expires  time.Time
	strategy string
	rows     []importRow
	invalid  int
}

type BatchImporter struct {
	mu       sync.Mutex
	service  ImportHostService
	dir      string
	ttl      time.Duration
	maxBytes int64
	previews map[string]storedPreview
	now      func() time.Time
}

func NewBatchImporter(service ImportHostService, dir string, ttl time.Duration, maxBytes int64) (*BatchImporter, error) {
	if service == nil || ttl <= 0 || maxBytes <= 0 {
		return nil, ErrInvalidInput
	}
	clean, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(clean, 0700); err != nil {
		return nil, err
	}
	return &BatchImporter{service: service, dir: clean, ttl: ttl, maxBytes: maxBytes, previews: map[string]storedPreview{}, now: time.Now}, nil
}

func (b *BatchImporter) Preview(ctx context.Context, actor *model.AppUser, file ImportFile) (*ImportPreview, error) {
	if actor == nil || actor.Role != model.RoleAdmin || file.Reader == nil {
		return nil, ErrAdminRequired
	}
	strategy := file.Strategy
	if strategy == "" {
		strategy = DuplicateSkip
	}
	if !validDuplicateStrategy(strategy) {
		return nil, ErrInvalidInput
	}
	if err := b.CleanupExpired(); err != nil {
		return nil, err
	}
	temp, err := os.CreateTemp(b.dir, "linux-import-*")
	if err != nil {
		return nil, err
	}
	path := temp.Name()
	_ = temp.Chmod(0600)
	defer os.Remove(path)
	written, copyErr := io.Copy(temp, io.LimitReader(file.Reader, b.maxBytes+1))
	closeErr := temp.Close()
	if copyErr != nil || closeErr != nil {
		return nil, errors.Join(copyErr, closeErr)
	}
	if written > b.maxBytes {
		return nil, ErrImportFormat
	}
	records, err := parseImportFile(path, file.Name)
	if err != nil {
		return nil, err
	}
	if len(records)-1 > MaxLinuxImportRows {
		return nil, ErrImportTooManyRows
	}
	hosts, err := b.service.ListHosts(ctx, actor)
	if err != nil {
		return nil, err
	}
	groups, err := b.service.ListCredentialGroups(ctx, actor)
	if err != nil {
		return nil, err
	}
	rows := buildImportRows(records, file.ColumnMapping, strategy, hosts, groups)
	token := randomImportToken()
	expires := b.now().Add(b.ttl)
	preview := &ImportPreview{Token: token, Strategy: strategy, Total: len(rows), ExpiresAt: expires, Rows: make([]ImportPreviewRow, len(rows)), TransactionPolicy: "per_row_atomic_continue_on_error"}
	invalid := 0
	for i := range rows {
		preview.Rows[i] = rows[i].preview
		if len(rows[i].preview.Issues) > 0 {
			invalid++
			preview.Invalid++
		} else {
			preview.Valid++
		}
		if rows[i].existingID > 0 {
			preview.Duplicates++
		}
	}
	b.mu.Lock()
	b.previews[token] = storedPreview{expires: expires, strategy: strategy, rows: rows, invalid: invalid}
	b.mu.Unlock()
	return preview, nil
}

func (b *BatchImporter) Confirm(ctx context.Context, actor *model.AppUser, token string) (*ImportResult, error) {
	if actor == nil || actor.Role != model.RoleAdmin {
		return nil, ErrAdminRequired
	}
	b.mu.Lock()
	preview, ok := b.previews[token]
	if ok {
		delete(b.previews, token)
	}
	b.mu.Unlock()
	if !ok || !b.now().Before(preview.expires) {
		return nil, ErrImportExpired
	}
	if preview.invalid > 0 {
		return nil, ErrImportHasErrors
	}
	result := &ImportResult{Total: len(preview.rows), Items: []ImportResultItem{}, TransactionPolicy: "per_row_atomic_continue_on_error"}
	for _, row := range preview.rows {
		item := ImportResultItem{Row: row.preview.Row}
		if row.preview.Action == DuplicateSkip {
			item.Status = "skipped"
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}
		var view *HostView
		var err error
		if row.existingID == 0 || row.preview.Action == DuplicateCreateAsDisabled {
			row.input.Enabled = row.preview.Action != DuplicateCreateAsDisabled
			view, err = b.service.CreateHost(ctx, actor, row.input)
			if err == nil {
				result.Created++
			}
		} else {
			update := importUpdate(row.input, preview.strategy == DuplicateReplaceConnectionConfig)
			view, err = b.service.UpdateHost(ctx, actor, row.existingID, update)
			if err == nil {
				result.Updated++
			}
		}
		if err != nil {
			item.Status = "failed"
			item.Code = "WRITE_FAILED"
			item.Message = "row could not be persisted"
			result.Failed++
		} else {
			item.Status = "success"
			item.HostID = view.ID
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (b *BatchImporter) CleanupExpired() error {
	now := b.now()
	b.mu.Lock()
	for key, p := range b.previews {
		if !now.Before(p.expires) {
			delete(b.previews, key)
		}
	}
	b.mu.Unlock()
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "linux-import-") {
			info, e := entry.Info()
			if e == nil && now.Sub(info.ModTime()) >= b.ttl {
				_ = os.Remove(filepath.Join(b.dir, entry.Name()))
			}
		}
	}
	return nil
}

func parseImportFile(path, name string) ([][]string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".xlsx" {
		book, err := excelize.OpenFile(path)
		if err != nil {
			return nil, ErrImportFormat
		}
		defer book.Close()
		sheets := book.GetSheetList()
		if len(sheets) == 0 {
			return nil, ErrImportFormat
		}
		return book.GetRows(sheets[0])
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	if ext == ".tsv" {
		reader.Comma = '\t'
	} else if ext != ".csv" {
		return nil, ErrImportFormat
	}
	reader.FieldsPerRecord = -1
	return reader.ReadAll()
}

func buildImportRows(records [][]string, mapping map[string]string, strategy string, hosts []HostView, groups []CredentialGroupView) []importRow {
	if len(records) < 2 {
		return []importRow{}
	}
	headers := map[string]int{}
	for i, h := range records[0] {
		headers[strings.ToLower(strings.TrimSpace(h))] = i
	}
	columns := map[string]int{}
	for _, field := range importFields {
		source := field
		if mapping[field] != "" {
			source = mapping[field]
		}
		if i, ok := headers[strings.ToLower(strings.TrimSpace(source))]; ok {
			columns[field] = i
		}
	}
	existing := map[string]HostView{}
	for _, h := range hosts {
		existing[importKey(pointerValue(h.Environment), h.Host, h.Port)] = h
	}
	groupByName := map[string]CredentialGroupView{}
	for _, g := range groups {
		groupByName[strings.ToLower(g.Name)] = g
	}
	seen := map[string]bool{}
	result := make([]importRow, 0, len(records)-1)
	for index, record := range records[1:] {
		values := func(k string) string {
			i, ok := columns[k]
			if !ok || i >= len(record) {
				return ""
			}
			return strings.TrimSpace(record[i])
		}
		port, _ := strconv.Atoi(values("port"))
		if port == 0 {
			port = 22
		}
		enabled := !strings.EqualFold(values("enabled"), "false")
		env := optionalString(values("environment"))
		auth := values("auth_type")
		if auth == "" {
			auth = model.LinuxAuthTypePassword
		}
		tags := splitImportList(values("tags"))
		tagsJSON, _ := json.Marshal(tags)
		input := HostInput{Name: values("name"), Host: values("host"), Port: port, Environment: env, SystemName: optionalString(values("system_name")), ComponentName: optionalString(values("component_name")), Username: optionalString(values("username")), AuthType: auth, Password: secretPointer(values("password")), PrivateKey: secretPointer(values("private_key")), PrivateKeyPassphrase: secretPointer(values("private_key_passphrase")), HostKeyPolicy: values("host_key_policy"), HostKeyFingerprint: optionalString(values("host_key_fingerprint")), Tags: tagsJSON, Enabled: enabled}
		preview := ImportPreviewRow{Row: index + 2, Name: input.Name, Host: input.Host, Port: port, Environment: pointerValue(env), AuthType: auth, CredentialGroupName: values("credential_group_name"), CredentialConfigured: input.Password != nil || input.PrivateKey != nil, GroupNames: splitImportList(values("group_names")), Tags: tags, Action: "create", Issues: []ImportIssue{}}
		if preview.CredentialGroupName != "" {
			if g, ok := groupByName[strings.ToLower(preview.CredentialGroupName)]; ok && g.Enabled {
				input.CredentialGroupID = &g.ID
				input.Username = nil
				input.Password = nil
				input.PrivateKey = nil
				input.PrivateKeyPassphrase = nil
				preview.CredentialConfigured = g.CredentialConfigured
			} else {
				preview.Issues = append(preview.Issues, ImportIssue{Row: preview.Row, Field: "credential_group_name", Code: "NOT_FOUND", Message: "credential group was not found or disabled"})
			}
		}
		key := importKey(preview.Environment, input.Host, port)
		if input.Name == "" || input.Host == "" || port < 1 || port > 65535 {
			preview.Issues = append(preview.Issues, ImportIssue{Row: preview.Row, Code: "INVALID_ROW", Message: "required host fields are missing"})
		}
		if input.CredentialGroupID == nil {
			if input.Username == nil || (auth == model.LinuxAuthTypePassword && input.Password == nil) || (auth == model.LinuxAuthTypePrivateKey && input.PrivateKey == nil) || (auth != model.LinuxAuthTypePassword && auth != model.LinuxAuthTypePrivateKey) {
				preview.Issues = append(preview.Issues, ImportIssue{Row: preview.Row, Code: "INVALID_CREDENTIAL", Message: "authentication configuration is incomplete"})
			}
		}
		if seen[key] {
			preview.Issues = append(preview.Issues, ImportIssue{Row: preview.Row, Code: "DUPLICATE_IN_FILE", Message: "duplicate environment, host and port in import"})
		}
		seen[key] = true
		row := importRow{preview: preview, input: input}
		if host, ok := existing[key]; ok {
			row.existingID = host.ID
			row.preview.ExistingHostID = &host.ID
			row.preview.Action = strategy
		}
		result = append(result, row)
	}
	return result
}

var importFields = []string{"name", "host", "port", "environment", "system_name", "component_name", "username", "auth_type", "credential_group_name", "password", "private_key", "private_key_passphrase", "host_key_policy", "host_key_fingerprint", "group_names", "tags", "enabled"}

func validDuplicateStrategy(v string) bool {
	return v == DuplicateSkip || v == DuplicateUpdateMetadata || v == DuplicateReplaceConnectionConfig || v == DuplicateCreateAsDisabled
}
func importKey(env, host string, port int) string {
	return strings.ToLower(strings.TrimSpace(env)) + "\x00" + strings.ToLower(strings.TrimSpace(host)) + "\x00" + strconv.Itoa(port)
}
func optionalString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
func secretPointer(v string) *string { return optionalString(v) }
func pointerValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func splitImportList(v string) []string {
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == '|' || r == ';' || r == ',' })
	out := []string{}
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
func randomImportToken() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
func importUpdate(v HostInput, credentials bool) HostUpdateInput {
	return HostUpdateInput{Name: &v.Name, Environment: v.Environment, EnvironmentSet: true, SystemName: v.SystemName, SystemNameSet: true, ComponentName: v.ComponentName, ComponentNameSet: true, Tags: v.Tags, TagsSet: true, Enabled: &v.Enabled, Username: v.Username, UsernameSet: credentials, AuthType: func() *string {
		if credentials {
			return &v.AuthType
		}
		return nil
	}(), Password: func() *string {
		if credentials {
			return v.Password
		}
		return nil
	}(), PrivateKey: func() *string {
		if credentials {
			return v.PrivateKey
		}
		return nil
	}(), PrivateKeyPassphrase: func() *string {
		if credentials {
			return v.PrivateKeyPassphrase
		}
		return nil
	}(), CredentialGroupID: v.CredentialGroupID, CredentialGroupIDSet: credentials}
}
