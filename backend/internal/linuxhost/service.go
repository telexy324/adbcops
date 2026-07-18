package linuxhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

const maxCredentialBytes = 64 * 1024

var (
	ErrForbidden            = errors.New("linux host access forbidden")
	ErrAdminRequired        = errors.New("admin role required")
	ErrInvalidInput         = errors.New("invalid linux host input")
	ErrCredentialConflict   = errors.New("direct credential and credential group conflict")
	ErrCredentialGroupScope = errors.New("credential group does not apply to linux host scope")
	ErrSensitiveAttribute   = errors.New("linux host attributes contain credential fields")
)

type Repository interface {
	ListLinuxHosts(ctx context.Context, includeDeleted bool) ([]model.LinuxHost, error)
	FindLinuxHostByID(ctx context.Context, id int64) (*model.LinuxHost, error)
	CreateLinuxHost(ctx context.Context, host *model.LinuxHost, credential *model.CredentialSecret) error
	UpdateLinuxHost(ctx context.Context, id int64, updates repository.LinuxHostUpdates, credential *model.CredentialSecret) (*model.LinuxHost, error)
	SetLinuxHostEnabled(ctx context.Context, id int64, enabled bool) (*model.LinuxHost, error)
	SoftDeleteLinuxHost(ctx context.Context, id int64) error
	ListCredentialGroups(ctx context.Context, enabledOnly bool) ([]model.CredentialGroup, error)
	FindCredentialGroupByID(ctx context.Context, id int64) (*model.CredentialGroup, error)
	CreateCredentialGroup(ctx context.Context, group *model.CredentialGroup, credential *model.CredentialSecret) error
	UpdateCredentialGroup(ctx context.Context, id int64, updates repository.CredentialGroupUpdates, credential *model.CredentialSecret) (*model.CredentialGroup, error)
	DeleteCredentialGroup(ctx context.Context, id int64) error
}

type SecretManager interface {
	Encrypt(plaintext string) (string, error)
}

type Service struct {
	repository Repository
	secrets    SecretManager
	keyVersion string
}

type HostInput struct {
	Name                 string
	Host                 string
	Port                 int
	Environment          *string
	SystemName           *string
	ComponentName        *string
	Username             *string
	AuthType             string
	Password             *string
	PrivateKey           *string
	PrivateKeyPassphrase *string
	CredentialGroupID    *int64
	HostKeyPolicy        string
	HostKeyFingerprint   *string
	ProfileID            *int64
	Tags                 json.RawMessage
	Attributes           json.RawMessage
	Enabled              bool
}

type HostUpdateInput struct {
	Name                  *string
	Host                  *string
	Port                  *int
	Environment           *string
	EnvironmentSet        bool
	SystemName            *string
	SystemNameSet         bool
	ComponentName         *string
	ComponentNameSet      bool
	Username              *string
	UsernameSet           bool
	AuthType              *string
	Password              *string
	PrivateKey            *string
	PrivateKeyPassphrase  *string
	CredentialGroupID     *int64
	CredentialGroupIDSet  bool
	HostKeyPolicy         *string
	HostKeyFingerprint    *string
	HostKeyFingerprintSet bool
	ProfileID             *int64
	ProfileIDSet          bool
	Tags                  json.RawMessage
	TagsSet               bool
	Attributes            json.RawMessage
	AttributesSet         bool
	Enabled               *bool
}

type CredentialGroupInput struct {
	Name                 string
	AuthType             string
	Username             string
	Password             *string
	PrivateKey           *string
	PrivateKeyPassphrase *string
	Scope                json.RawMessage
	Enabled              bool
}

type CredentialGroupUpdateInput struct {
	Name                 *string
	AuthType             *string
	Username             *string
	Password             *string
	PrivateKey           *string
	PrivateKeyPassphrase *string
	Scope                json.RawMessage
	ScopeSet             bool
	Enabled              *bool
}

type HostView struct {
	ID                   int64           `json:"id"`
	DataSourceID         *int64          `json:"dataSourceId,omitempty"`
	Name                 string          `json:"name"`
	Host                 string          `json:"host"`
	Port                 int             `json:"port"`
	Environment          *string         `json:"environment,omitempty"`
	SystemName           *string         `json:"systemName,omitempty"`
	ComponentName        *string         `json:"componentName,omitempty"`
	Username             *string         `json:"username,omitempty"`
	AuthType             string          `json:"authType"`
	CredentialGroupID    *int64          `json:"credentialGroupId,omitempty"`
	CredentialConfigured bool            `json:"credentialConfigured"`
	HostKeyPolicy        string          `json:"hostKeyPolicy"`
	HostKeyAlgorithm     *string         `json:"hostKeyAlgorithm,omitempty"`
	HostKeyFingerprint   *string         `json:"hostKeyFingerprint,omitempty"`
	ProfileID            *int64          `json:"profileId,omitempty"`
	Tags                 json.RawMessage `json:"tags,omitempty"`
	Attributes           json.RawMessage `json:"attributes,omitempty"`
	Enabled              bool            `json:"enabled"`
	ConnectionStatus     string          `json:"connectionStatus"`
	LastTestAt           *time.Time      `json:"lastTestAt,omitempty"`
	LastSuccessAt        *time.Time      `json:"lastSuccessAt,omitempty"`
	LastErrorCode        *string         `json:"lastErrorCode,omitempty"`
	LastErrorMessage     *string         `json:"lastErrorMessage,omitempty"`
	MachineIdentityHash  *string         `json:"machineIdentityHash,omitempty"`
	DetectedPlatform     json.RawMessage `json:"detectedPlatform,omitempty"`
	CreatedBy            *int64          `json:"createdBy,omitempty"`
	CreatedAt            time.Time       `json:"createdAt"`
	UpdatedAt            time.Time       `json:"updatedAt"`
}

type CredentialGroupView struct {
	ID                   int64           `json:"id"`
	Name                 string          `json:"name"`
	AuthType             string          `json:"authType"`
	Username             string          `json:"username"`
	CredentialConfigured bool            `json:"credentialConfigured"`
	Scope                json.RawMessage `json:"scope,omitempty"`
	Enabled              bool            `json:"enabled"`
	Version              int             `json:"version"`
	CreatedBy            *int64          `json:"createdBy,omitempty"`
	CreatedAt            time.Time       `json:"createdAt"`
	UpdatedAt            time.Time       `json:"updatedAt"`
}

func NewService(repository Repository, secrets SecretManager, keyVersion string) *Service {
	return &Service{repository: repository, secrets: secrets, keyVersion: keyVersion}
}

func (s *Service) ListHosts(ctx context.Context, actor *model.AppUser) ([]HostView, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	hosts, err := s.repository.ListLinuxHosts(ctx, false)
	if err != nil {
		return nil, err
	}
	views := make([]HostView, 0, len(hosts))
	for i := range hosts {
		if actor.Role != model.RoleAdmin && !hosts[i].Enabled {
			continue
		}
		views = append(views, hostToView(&hosts[i]))
	}
	return views, nil
}

func (s *Service) GetHost(ctx context.Context, actor *model.AppUser, id int64) (*HostView, error) {
	if actor == nil || id <= 0 {
		return nil, ErrInvalidInput
	}
	host, err := s.repository.FindLinuxHostByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if actor.Role != model.RoleAdmin && !host.Enabled {
		return nil, ErrForbidden
	}
	view := hostToView(host)
	return &view, nil
}

func (s *Service) CreateHost(ctx context.Context, actor *model.AppUser, input HostInput) (*HostView, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}
	normalized, credential, err := s.normalizeHostInput(ctx, actor, input)
	if err != nil {
		return nil, err
	}
	host := &model.LinuxHost{
		Name: normalized.Name, Host: normalized.Host, Port: normalized.Port,
		Environment: normalized.Environment, SystemName: normalized.SystemName, ComponentName: normalized.ComponentName,
		Username: normalized.Username, AuthType: normalized.AuthType, CredentialGroupID: normalized.CredentialGroupID,
		HostKeyPolicy: normalized.HostKeyPolicy, HostKeyFingerprint: normalized.HostKeyFingerprint,
		ProfileID: normalized.ProfileID, Tags: normalized.Tags, Attributes: normalized.Attributes,
		Enabled: normalized.Enabled, ConnectionStatus: model.LinuxConnectionStatusUnknown, CreatedBy: actorID(actor),
	}
	if err := s.repository.CreateLinuxHost(ctx, host, credential); err != nil {
		return nil, err
	}
	view := hostToView(host)
	return &view, nil
}

func (s *Service) UpdateHost(ctx context.Context, actor *model.AppUser, id int64, input HostUpdateInput) (*HostView, error) {
	if err := requireAdmin(actor); err != nil || id <= 0 {
		if err != nil {
			return nil, err
		}
		return nil, ErrInvalidInput
	}
	current, err := s.repository.FindLinuxHostByID(ctx, id)
	if err != nil {
		return nil, err
	}
	updates, credential, err := s.normalizeHostUpdate(ctx, actor, current, input)
	if err != nil {
		return nil, err
	}
	updated, err := s.repository.UpdateLinuxHost(ctx, id, updates, credential)
	if err != nil {
		return nil, err
	}
	view := hostToView(updated)
	return &view, nil
}

func (s *Service) SetHostEnabled(ctx context.Context, actor *model.AppUser, id int64, enabled bool) (*HostView, error) {
	if err := requireAdmin(actor); err != nil || id <= 0 {
		if err != nil {
			return nil, err
		}
		return nil, ErrInvalidInput
	}
	host, err := s.repository.SetLinuxHostEnabled(ctx, id, enabled)
	if err != nil {
		return nil, err
	}
	view := hostToView(host)
	return &view, nil
}

func (s *Service) DeleteHost(ctx context.Context, actor *model.AppUser, id int64) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}
	if id <= 0 {
		return ErrInvalidInput
	}
	return s.repository.SoftDeleteLinuxHost(ctx, id)
}

func (s *Service) ListCredentialGroups(ctx context.Context, actor *model.AppUser) ([]CredentialGroupView, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}
	groups, err := s.repository.ListCredentialGroups(ctx, false)
	if err != nil {
		return nil, err
	}
	views := make([]CredentialGroupView, 0, len(groups))
	for i := range groups {
		views = append(views, credentialGroupToView(&groups[i]))
	}
	return views, nil
}

func (s *Service) GetCredentialGroup(ctx context.Context, actor *model.AppUser, id int64) (*CredentialGroupView, error) {
	if err := requireAdmin(actor); err != nil || id <= 0 {
		if err != nil {
			return nil, err
		}
		return nil, ErrInvalidInput
	}
	group, err := s.repository.FindCredentialGroupByID(ctx, id)
	if err != nil {
		return nil, err
	}
	view := credentialGroupToView(group)
	return &view, nil
}

func (s *Service) CreateCredentialGroup(ctx context.Context, actor *model.AppUser, input CredentialGroupInput) (*CredentialGroupView, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}
	name, err := normalizeRequired(input.Name, 120)
	if err != nil {
		return nil, err
	}
	authType, err := normalizeAuthType(input.AuthType)
	if err != nil {
		return nil, err
	}
	username, err := normalizeRequired(input.Username, 255)
	if err != nil {
		return nil, err
	}
	scope, err := normalizeScope(input.Scope)
	if err != nil {
		return nil, err
	}
	credential, err := s.encryptCredential(authType, username, input.Password, input.PrivateKey, input.PrivateKeyPassphrase, actorID(actor))
	if err != nil {
		return nil, err
	}
	group := &model.CredentialGroup{
		Name: name, CredentialType: authType, Username: username, Scope: scope,
		Enabled: input.Enabled, Version: 1, CreatedBy: actorID(actor),
	}
	if err := s.repository.CreateCredentialGroup(ctx, group, credential); err != nil {
		return nil, err
	}
	view := credentialGroupToView(group)
	return &view, nil
}

func (s *Service) UpdateCredentialGroup(ctx context.Context, actor *model.AppUser, id int64, input CredentialGroupUpdateInput) (*CredentialGroupView, error) {
	if err := requireAdmin(actor); err != nil || id <= 0 {
		if err != nil {
			return nil, err
		}
		return nil, ErrInvalidInput
	}
	current, err := s.repository.FindCredentialGroupByID(ctx, id)
	if err != nil {
		return nil, err
	}
	updates := repository.CredentialGroupUpdates{Enabled: input.Enabled}
	if input.Name != nil {
		value, err := normalizeRequired(*input.Name, 120)
		if err != nil {
			return nil, err
		}
		updates.Name = &value
	}
	authType := current.CredentialType
	if input.AuthType != nil {
		authType, err = normalizeAuthType(*input.AuthType)
		if err != nil {
			return nil, err
		}
		updates.CredentialType = &authType
	}
	username := current.Username
	if input.Username != nil {
		username, err = normalizeRequired(*input.Username, 255)
		if err != nil {
			return nil, err
		}
		updates.Username = &username
	}
	if input.ScopeSet {
		updates.Scope, err = normalizeScope(input.Scope)
		if err != nil {
			return nil, err
		}
		updates.ScopeSet = true
	}
	hasCredential := input.Password != nil || input.PrivateKey != nil || input.PrivateKeyPassphrase != nil
	if input.AuthType != nil && authType != current.CredentialType && !hasCredential {
		return nil, ErrInvalidInput
	}
	var credential *model.CredentialSecret
	if hasCredential {
		credential, err = s.encryptCredential(authType, username, input.Password, input.PrivateKey, input.PrivateKeyPassphrase, actorID(actor))
		if err != nil {
			return nil, err
		}
	}
	updated, err := s.repository.UpdateCredentialGroup(ctx, id, updates, credential)
	if err != nil {
		return nil, err
	}
	view := credentialGroupToView(updated)
	return &view, nil
}

func (s *Service) DeleteCredentialGroup(ctx context.Context, actor *model.AppUser, id int64) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}
	if id <= 0 {
		return ErrInvalidInput
	}
	if err := s.repository.DeleteCredentialGroup(ctx, id); err != nil {
		return err
	}
	return nil
}

func (s *Service) normalizeHostInput(ctx context.Context, actor *model.AppUser, input HostInput) (HostInput, *model.CredentialSecret, error) {
	var err error
	if input.Name, err = normalizeRequired(input.Name, 120); err != nil {
		return HostInput{}, nil, err
	}
	if input.Host, err = normalizeHost(input.Host); err != nil {
		return HostInput{}, nil, err
	}
	if input.Port == 0 {
		input.Port = 22
	}
	if input.Port < 1 || input.Port > 65535 {
		return HostInput{}, nil, ErrInvalidInput
	}
	if input.Environment, err = normalizeOptional(input.Environment, 50); err != nil {
		return HostInput{}, nil, err
	}
	if input.SystemName, err = normalizeOptional(input.SystemName, 100); err != nil {
		return HostInput{}, nil, err
	}
	if input.ComponentName, err = normalizeOptional(input.ComponentName, 100); err != nil {
		return HostInput{}, nil, err
	}
	if input.AuthType, err = normalizeAuthType(input.AuthType); err != nil {
		return HostInput{}, nil, err
	}
	if input.HostKeyPolicy == "" {
		input.HostKeyPolicy = model.LinuxHostKeyPolicyStrict
	}
	if input.HostKeyPolicy, err = normalizeHostKeyPolicy(input.HostKeyPolicy); err != nil {
		return HostInput{}, nil, err
	}
	if input.HostKeyFingerprint, err = normalizeOptional(input.HostKeyFingerprint, 255); err != nil {
		return HostInput{}, nil, err
	}
	if input.ProfileID != nil && *input.ProfileID <= 0 {
		return HostInput{}, nil, ErrInvalidInput
	}
	if input.Tags, err = normalizeJSONArray(input.Tags); err != nil {
		return HostInput{}, nil, err
	}
	if input.Attributes, err = normalizeJSONObject(input.Attributes, true); err != nil {
		return HostInput{}, nil, err
	}
	hasDirect := input.Password != nil || input.PrivateKey != nil || input.PrivateKeyPassphrase != nil
	if input.CredentialGroupID != nil {
		if *input.CredentialGroupID <= 0 || hasDirect {
			return HostInput{}, nil, ErrCredentialConflict
		}
		group, err := s.validCredentialGroup(ctx, *input.CredentialGroupID, input.AuthType, input.Environment, input.SystemName)
		if err != nil {
			return HostInput{}, nil, err
		}
		input.Username = nil
		input.AuthType = group.CredentialType
		return input, nil, nil
	}
	username, err := normalizeOptional(input.Username, 255)
	if err != nil || username == nil {
		return HostInput{}, nil, ErrInvalidInput
	}
	input.Username = username
	credential, err := s.encryptCredential(input.AuthType, *username, input.Password, input.PrivateKey, input.PrivateKeyPassphrase, actorID(actor))
	if err != nil {
		return HostInput{}, nil, err
	}
	return input, credential, nil
}

func (s *Service) normalizeHostUpdate(ctx context.Context, actor *model.AppUser, current *model.LinuxHost, input HostUpdateInput) (repository.LinuxHostUpdates, *model.CredentialSecret, error) {
	updates := repository.LinuxHostUpdates{Enabled: input.Enabled}
	var err error
	if input.Name != nil {
		value, e := normalizeRequired(*input.Name, 120)
		if e != nil {
			return updates, nil, e
		}
		updates.Name = &value
	}
	if input.Host != nil {
		value, e := normalizeHost(*input.Host)
		if e != nil {
			return updates, nil, e
		}
		updates.Host = &value
	}
	if input.Port != nil {
		if *input.Port < 1 || *input.Port > 65535 {
			return updates, nil, ErrInvalidInput
		}
		updates.Port = input.Port
	}
	updates.EnvironmentSet = input.EnvironmentSet
	if input.EnvironmentSet {
		updates.Environment, err = normalizeOptional(input.Environment, 50)
		if err != nil {
			return updates, nil, err
		}
	}
	updates.SystemNameSet = input.SystemNameSet
	if input.SystemNameSet {
		updates.SystemName, err = normalizeOptional(input.SystemName, 100)
		if err != nil {
			return updates, nil, err
		}
	}
	updates.ComponentNameSet = input.ComponentNameSet
	if input.ComponentNameSet {
		updates.ComponentName, err = normalizeOptional(input.ComponentName, 100)
		if err != nil {
			return updates, nil, err
		}
	}
	updates.UsernameSet = input.UsernameSet
	if input.UsernameSet {
		updates.Username, err = normalizeOptional(input.Username, 255)
		if err != nil {
			return updates, nil, err
		}
	}
	authType := current.AuthType
	if input.AuthType != nil {
		authType, err = normalizeAuthType(*input.AuthType)
		if err != nil {
			return updates, nil, err
		}
		updates.AuthType = &authType
	}
	if input.HostKeyPolicy != nil {
		value, e := normalizeHostKeyPolicy(*input.HostKeyPolicy)
		if e != nil {
			return updates, nil, e
		}
		updates.HostKeyPolicy = &value
	}
	updates.HostKeyFingerprintSet = input.HostKeyFingerprintSet
	if input.HostKeyFingerprintSet {
		updates.HostKeyFingerprint, err = normalizeOptional(input.HostKeyFingerprint, 255)
		if err != nil {
			return updates, nil, err
		}
	}
	updates.ProfileIDSet, updates.ProfileID = input.ProfileIDSet, input.ProfileID
	if input.ProfileIDSet && input.ProfileID != nil && *input.ProfileID <= 0 {
		return updates, nil, ErrInvalidInput
	}
	updates.TagsSet = input.TagsSet
	if input.TagsSet {
		updates.Tags, err = normalizeJSONArray(input.Tags)
		if err != nil {
			return updates, nil, err
		}
	}
	updates.AttributesSet = input.AttributesSet
	if input.AttributesSet {
		updates.Attributes, err = normalizeJSONObject(input.Attributes, true)
		if err != nil {
			return updates, nil, err
		}
	}
	hasDirect := input.Password != nil || input.PrivateKey != nil || input.PrivateKeyPassphrase != nil
	if input.CredentialGroupIDSet && input.CredentialGroupID != nil && hasDirect {
		return updates, nil, ErrCredentialConflict
	}
	effectiveEnvironment := current.Environment
	if input.EnvironmentSet {
		effectiveEnvironment = updates.Environment
	}
	effectiveSystem := current.SystemName
	if input.SystemNameSet {
		effectiveSystem = updates.SystemName
	}
	effectiveGroupID := current.CredentialGroupID
	if input.CredentialGroupIDSet {
		effectiveGroupID = input.CredentialGroupID
	}
	if effectiveGroupID != nil {
		if *effectiveGroupID <= 0 || hasDirect || input.UsernameSet {
			return updates, nil, ErrCredentialConflict
		}
		if _, err := s.validCredentialGroup(ctx, *effectiveGroupID, authType, effectiveEnvironment, effectiveSystem); err != nil {
			return updates, nil, err
		}
		updates.CredentialGroupIDSet = input.CredentialGroupIDSet
		updates.CredentialGroupID = input.CredentialGroupID
		if input.CredentialGroupIDSet {
			updates.ClearDirectCredential = true
		}
		if input.AuthType != nil && authType != current.AuthType && !input.CredentialGroupIDSet {
			return updates, nil, ErrInvalidInput
		}
		return updates, nil, nil
	}
	updates.CredentialGroupIDSet = input.CredentialGroupIDSet
	updates.CredentialGroupID = input.CredentialGroupID
	if !hasDirect {
		if input.CredentialGroupIDSet && current.CredentialGroupID != nil {
			return updates, nil, ErrInvalidInput
		}
		if input.AuthType != nil && authType != current.AuthType {
			return updates, nil, ErrInvalidInput
		}
		return updates, nil, nil
	}
	username := current.Username
	if input.UsernameSet {
		username = updates.Username
	}
	if username == nil {
		return updates, nil, ErrInvalidInput
	}
	credential, err := s.encryptCredential(authType, *username, input.Password, input.PrivateKey, input.PrivateKeyPassphrase, actorID(actor))
	if err != nil {
		return updates, nil, err
	}
	updates.CredentialGroupIDSet = true
	updates.CredentialGroupID = nil
	return updates, credential, nil
}

func (s *Service) validCredentialGroup(ctx context.Context, id int64, authType string, environment, systemName *string) (*model.CredentialGroup, error) {
	group, err := s.repository.FindCredentialGroupByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !group.Enabled || group.CredentialType != authType {
		return nil, ErrInvalidInput
	}
	if !scopeContains(group.Scope, environment, systemName) {
		return nil, ErrCredentialGroupScope
	}
	return group, nil
}

func (s *Service) encryptCredential(authType, username string, password, privateKey, passphrase *string, createdBy *int64) (*model.CredentialSecret, error) {
	if s.secrets == nil {
		return nil, ErrInvalidInput
	}
	payload := map[string]string{"username": username}
	switch authType {
	case model.LinuxAuthTypePassword:
		if password == nil || strings.TrimSpace(*password) == "" || privateKey != nil || passphrase != nil {
			return nil, ErrInvalidInput
		}
		payload["password"] = *password
	case model.LinuxAuthTypePrivateKey:
		if privateKey == nil || strings.TrimSpace(*privateKey) == "" || password != nil {
			return nil, ErrInvalidInput
		}
		payload["privateKey"] = *privateKey
		if passphrase != nil {
			payload["privateKeyPassphrase"] = *passphrase
		}
	default:
		return nil, ErrInvalidInput
	}
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > maxCredentialBytes {
		return nil, ErrInvalidInput
	}
	encrypted, err := s.secrets.Encrypt(string(raw))
	if err != nil {
		return nil, fmt.Errorf("encrypt linux credential: %w", err)
	}
	keyVersion := strings.TrimSpace(s.keyVersion)
	var version *string
	if keyVersion != "" {
		version = &keyVersion
	}
	return &model.CredentialSecret{SecretType: "linux_" + authType, EncryptedPayload: encrypted, KeyVersion: version, CreatedBy: createdBy}, nil
}

func normalizeRequired(value string, limit int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > limit {
		return "", ErrInvalidInput
	}
	return value, nil
}

func normalizeOptional(value *string, limit int) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(normalized) > limit {
		return nil, ErrInvalidInput
	}
	return &normalized, nil
}

func normalizeHost(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 || strings.ContainsAny(value, " /\\?#@") {
		return "", ErrInvalidInput
	}
	if net.ParseIP(strings.Trim(value, "[]")) != nil {
		return strings.Trim(value, "[]"), nil
	}
	labels := strings.Split(value, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrInvalidInput
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' {
				return "", ErrInvalidInput
			}
		}
	}
	return strings.ToLower(value), nil
}

func normalizeAuthType(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value != model.LinuxAuthTypePassword && value != model.LinuxAuthTypePrivateKey {
		return "", ErrInvalidInput
	}
	return value, nil
}

func normalizeHostKeyPolicy(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value != model.LinuxHostKeyPolicyStrict && value != model.LinuxHostKeyPolicyTrustOnFirstUse {
		return "", ErrInvalidInput
	}
	return value, nil
}

func normalizeScope(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if err := model.ValidateCredentialGroupScope(raw); err != nil {
		return nil, ErrInvalidInput
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil, ErrInvalidInput
	}
	return json.Marshal(value)
}

func normalizeJSONArray(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		return nil, ErrInvalidInput
	}
	seen := map[string]struct{}{}
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
		if values[i] == "" || utf8.RuneCountInString(values[i]) > 100 {
			return nil, ErrInvalidInput
		}
		if _, exists := seen[values[i]]; exists {
			return nil, ErrInvalidInput
		}
		seen[values[i]] = struct{}{}
	}
	return json.Marshal(values)
}

func normalizeJSONObject(raw json.RawMessage, rejectSensitive bool) ([]byte, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return nil, ErrInvalidInput
	}
	if rejectSensitive && containsSensitive(value) {
		return nil, ErrSensitiveAttribute
	}
	return json.Marshal(value)
}

func containsSensitive(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
			if strings.Contains(normalized, "password") || strings.Contains(normalized, "privatekey") || strings.Contains(normalized, "passphrase") || strings.Contains(normalized, "secret") {
				return true
			}
			if containsSensitive(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsSensitive(child) {
				return true
			}
		}
	}
	return false
}

func scopeContains(raw json.RawMessage, environment, systemName *string) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return true
	}
	var scope model.CredentialGroupScope
	if json.Unmarshal(raw, &scope) != nil {
		return false
	}
	return scopeValueAllowed(scope.Environments, environment) && scopeValueAllowed(scope.SystemNames, systemName)
}

func scopeValueAllowed(allowed []string, actual *string) bool {
	if len(allowed) == 0 {
		return true
	}
	if actual == nil {
		return false
	}
	for _, value := range allowed {
		if value == *actual {
			return true
		}
	}
	return false
}

func requireAdmin(actor *model.AppUser) error {
	if actor == nil {
		return ErrForbidden
	}
	if actor.Role != model.RoleAdmin {
		return ErrAdminRequired
	}
	return nil
}

func actorID(actor *model.AppUser) *int64 { id := actor.ID; return &id }

func hostToView(host *model.LinuxHost) HostView {
	return HostView{
		ID: host.ID, DataSourceID: host.DataSourceID, Name: host.Name, Host: host.Host, Port: host.Port,
		Environment: host.Environment, SystemName: host.SystemName, ComponentName: host.ComponentName,
		Username: host.Username, AuthType: host.AuthType, CredentialGroupID: host.CredentialGroupID,
		CredentialConfigured: host.CredentialID != nil || host.CredentialGroupID != nil,
		HostKeyPolicy:        host.HostKeyPolicy, HostKeyAlgorithm: host.HostKeyAlgorithm, HostKeyFingerprint: host.HostKeyFingerprint,
		ProfileID: host.ProfileID, Tags: host.Tags, Attributes: host.Attributes, Enabled: host.Enabled,
		ConnectionStatus: host.ConnectionStatus, LastTestAt: host.LastTestAt, LastSuccessAt: host.LastSuccessAt,
		LastErrorCode: host.LastErrorCode, LastErrorMessage: host.LastErrorMessage,
		MachineIdentityHash: host.MachineIdentityHash, DetectedPlatform: host.DetectedPlatform,
		CreatedBy: host.CreatedBy, CreatedAt: host.CreatedAt, UpdatedAt: host.UpdatedAt,
	}
}

func credentialGroupToView(group *model.CredentialGroup) CredentialGroupView {
	return CredentialGroupView{
		ID: group.ID, Name: group.Name, AuthType: group.CredentialType, Username: group.Username,
		CredentialConfigured: group.CredentialID > 0, Scope: group.Scope, Enabled: group.Enabled,
		Version: group.Version, CreatedBy: group.CreatedBy, CreatedAt: group.CreatedAt, UpdatedAt: group.UpdatedAt,
	}
}
