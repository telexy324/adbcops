package model

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	LinuxAuthTypePassword   = "password"
	LinuxAuthTypePrivateKey = "private_key"

	LinuxHostKeyPolicyStrict          = "strict"
	LinuxHostKeyPolicyTrustOnFirstUse = "trust_on_first_use"
	LinuxHostKeyPolicyInsecure        = "insecure_skip_verify"

	LinuxConnectionStatusUnknown = "unknown"

	LinuxHostKeyStatusUnverified = "unverified"
	LinuxHostKeyStatusPending    = "pending"
	LinuxHostKeyStatusTrusted    = "trusted"
	LinuxHostKeyStatusMismatch   = "mismatch"

	LinuxConnectionStatusHostKeyMismatch = "host_key_mismatch"
	LinuxHostKeyMismatchErrorCode        = "HOST_KEY_MISMATCH"
)

var ErrInvalidCredentialGroupScope = errors.New("invalid credential group scope")

type LinuxHost struct {
	ID                        int64             `gorm:"column:id;primaryKey" json:"id"`
	DataSourceID              *int64            `gorm:"column:data_source_id" json:"dataSourceId,omitempty"`
	Name                      string            `gorm:"column:name;size:120;not null" json:"name"`
	Host                      string            `gorm:"column:host;size:255;not null" json:"host"`
	Port                      int               `gorm:"column:port;not null" json:"port"`
	Environment               *string           `gorm:"column:environment;size:50" json:"environment,omitempty"`
	SystemName                *string           `gorm:"column:system_name;size:100" json:"systemName,omitempty"`
	ComponentName             *string           `gorm:"column:component_name;size:100" json:"componentName,omitempty"`
	Username                  *string           `gorm:"column:username;size:255" json:"username,omitempty"`
	AuthType                  string            `gorm:"column:auth_type;size:50;not null" json:"authType"`
	CredentialID              *int64            `gorm:"column:credential_id" json:"-"`
	Credential                *CredentialSecret `gorm:"foreignKey:CredentialID" json:"-"`
	CredentialGroupID         *int64            `gorm:"column:credential_group_id" json:"credentialGroupId,omitempty"`
	HostKeyPolicy             string            `gorm:"column:host_key_policy;size:50;not null" json:"hostKeyPolicy"`
	HostKeyAlgorithm          *string           `gorm:"column:host_key_algorithm;size:100" json:"hostKeyAlgorithm,omitempty"`
	HostKeyFingerprint        *string           `gorm:"column:host_key_fingerprint;size:255" json:"hostKeyFingerprint,omitempty"`
	HostKeyStatus             string            `gorm:"column:host_key_status;size:30;not null" json:"hostKeyStatus"`
	PendingHostKeyAlgorithm   *string           `gorm:"column:pending_host_key_algorithm;size:100" json:"pendingHostKeyAlgorithm,omitempty"`
	PendingHostKeyFingerprint *string           `gorm:"column:pending_host_key_fingerprint;size:255" json:"pendingHostKeyFingerprint,omitempty"`
	HostKeyObservedAt         *time.Time        `gorm:"column:host_key_observed_at" json:"hostKeyObservedAt,omitempty"`
	HostKeyConfirmedAt        *time.Time        `gorm:"column:host_key_confirmed_at" json:"hostKeyConfirmedAt,omitempty"`
	HostKeyConfirmedBy        *int64            `gorm:"column:host_key_confirmed_by" json:"hostKeyConfirmedBy,omitempty"`
	ProfileID                 *int64            `gorm:"column:profile_id" json:"profileId,omitempty"`
	Tags                      json.RawMessage   `gorm:"column:tags;type:jsonb" json:"tags,omitempty"`
	Attributes                json.RawMessage   `gorm:"column:attributes;type:jsonb" json:"attributes,omitempty"`
	Enabled                   bool              `gorm:"column:enabled;not null" json:"enabled"`
	ConnectionStatus          string            `gorm:"column:connection_status;size:30" json:"connectionStatus"`
	LastTestAt                *time.Time        `gorm:"column:last_test_at" json:"lastTestAt,omitempty"`
	LastSuccessAt             *time.Time        `gorm:"column:last_success_at" json:"lastSuccessAt,omitempty"`
	LastErrorCode             *string           `gorm:"column:last_error_code;size:80" json:"lastErrorCode,omitempty"`
	LastErrorMessage          *string           `gorm:"column:last_error_message" json:"lastErrorMessage,omitempty"`
	MachineIdentityHash       *string           `gorm:"column:machine_identity_hash;size:255" json:"machineIdentityHash,omitempty"`
	DetectedPlatform          json.RawMessage   `gorm:"column:detected_platform;type:jsonb" json:"detectedPlatform,omitempty"`
	CreatedBy                 *int64            `gorm:"column:created_by" json:"createdBy,omitempty"`
	CreatedAt                 time.Time         `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt                 time.Time         `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
	DeletedAt                 *time.Time        `gorm:"column:deleted_at" json:"deletedAt,omitempty"`
}

func (LinuxHost) TableName() string { return "linux_host" }

type CredentialGroup struct {
	ID                   int64             `gorm:"column:id;primaryKey" json:"id"`
	Name                 string            `gorm:"column:name;size:120;not null" json:"name"`
	CredentialType       string            `gorm:"column:credential_type;size:50;not null" json:"credentialType"`
	Username             string            `gorm:"column:username;size:255;not null" json:"username"`
	CredentialID         int64             `gorm:"column:credential_id;not null" json:"-"`
	Credential           *CredentialSecret `gorm:"foreignKey:CredentialID" json:"-"`
	CredentialConfigured bool              `gorm:"-" json:"credentialConfigured"`
	Scope                json.RawMessage   `gorm:"column:scope;type:jsonb" json:"scope,omitempty"`
	Enabled              bool              `gorm:"column:enabled;not null" json:"enabled"`
	Version              int               `gorm:"column:version;not null" json:"version"`
	CreatedBy            *int64            `gorm:"column:created_by" json:"createdBy,omitempty"`
	CreatedAt            time.Time         `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt            time.Time         `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (CredentialGroup) TableName() string { return "credential_group" }

type CredentialGroupScope struct {
	Environments []string `json:"environments,omitempty"`
	SystemNames  []string `json:"systemNames,omitempty"`
}

func ValidateCredentialGroupScope(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return ErrInvalidCredentialGroupScope
	}
	for key := range object {
		if key != "environments" && key != "systemNames" {
			return ErrInvalidCredentialGroupScope
		}
	}
	var scope CredentialGroupScope
	if json.Unmarshal(raw, &scope) != nil || !validScopeValues(scope.Environments) || !validScopeValues(scope.SystemNames) {
		return ErrInvalidCredentialGroupScope
	}
	return nil
}

func validScopeValues(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

type LinuxHostGroup struct {
	ID          int64           `gorm:"column:id;primaryKey" json:"id"`
	Name        string          `gorm:"column:name;size:120;not null" json:"name"`
	Description *string         `gorm:"column:description" json:"description,omitempty"`
	Environment *string         `gorm:"column:environment;size:50" json:"environment,omitempty"`
	SystemName  *string         `gorm:"column:system_name;size:100" json:"systemName,omitempty"`
	Tags        json.RawMessage `gorm:"column:tags;type:jsonb" json:"tags,omitempty"`
	CreatedBy   *int64          `gorm:"column:created_by" json:"createdBy,omitempty"`
	CreatedAt   time.Time       `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt   time.Time       `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
	Members     []LinuxHost     `gorm:"many2many:linux_host_group_member;joinForeignKey:GroupID;joinReferences:HostID" json:"members,omitempty"`
}

func (LinuxHostGroup) TableName() string { return "linux_host_group" }

type LinuxHostGroupMember struct {
	GroupID   int64     `gorm:"column:group_id;primaryKey" json:"groupId"`
	HostID    int64     `gorm:"column:host_id;primaryKey" json:"hostId"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (LinuxHostGroupMember) TableName() string { return "linux_host_group_member" }

type LinuxHostProfile struct {
	ID                     int64           `gorm:"column:id;primaryKey" json:"id"`
	Name                   string          `gorm:"column:name;size:120;not null" json:"name"`
	Description            *string         `gorm:"column:description" json:"description,omitempty"`
	Collectors             json.RawMessage `gorm:"column:collectors;type:jsonb;not null" json:"collectors"`
	CriticalServices       json.RawMessage `gorm:"column:critical_services;type:jsonb" json:"criticalServices,omitempty"`
	ExpectedListeningPorts json.RawMessage `gorm:"column:expected_listening_ports;type:jsonb" json:"expectedListeningPorts,omitempty"`
	FilesystemRules        json.RawMessage `gorm:"column:filesystem_rules;type:jsonb" json:"filesystemRules,omitempty"`
	ProcessRules           json.RawMessage `gorm:"column:process_rules;type:jsonb" json:"processRules,omitempty"`
	CustomThresholds       json.RawMessage `gorm:"column:custom_thresholds;type:jsonb" json:"customThresholds,omitempty"`
	BuiltIn                bool            `gorm:"column:built_in;not null" json:"builtIn"`
	Enabled                bool            `gorm:"column:enabled;not null" json:"enabled"`
	CreatedBy              *int64          `gorm:"column:created_by" json:"createdBy,omitempty"`
	CreatedAt              time.Time       `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt              time.Time       `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (LinuxHostProfile) TableName() string { return "linux_host_profile" }
