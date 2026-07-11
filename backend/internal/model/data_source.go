package model

import "time"

const (
	DataSourceTypeElasticsearch = "elasticsearch"
	DataSourceTypeOpenSearch    = "opensearch"
	DataSourceTypePrometheus    = "prometheus"
	DataSourceTypeKubernetes    = "kubernetes"
	DataSourceTypeSSH           = "ssh"
	DataSourceTypeHTTP          = "http"
)

type CredentialSecret struct {
	ID               int64     `gorm:"column:id;primaryKey" json:"id"`
	SecretType       string    `gorm:"column:secret_type;size:50;not null" json:"secretType"`
	EncryptedPayload string    `gorm:"column:encrypted_payload;not null" json:"-"`
	KeyVersion       *string   `gorm:"column:key_version;size:50" json:"keyVersion,omitempty"`
	CreatedBy        *int64    `gorm:"column:created_by" json:"createdBy,omitempty"`
	CreatedAt        time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt        time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (CredentialSecret) TableName() string {
	return "credential_secret"
}

type DataSource struct {
	ID            int64             `gorm:"column:id;primaryKey" json:"id"`
	Name          string            `gorm:"column:name;size:120;not null" json:"name"`
	SourceType    string            `gorm:"column:source_type;size:50;not null" json:"sourceType"`
	Environment   *string           `gorm:"column:environment;size:50" json:"environment,omitempty"`
	SystemName    *string           `gorm:"column:system_name;size:100" json:"systemName,omitempty"`
	ComponentName *string           `gorm:"column:component_name;size:100" json:"componentName,omitempty"`
	Config        []byte            `gorm:"column:config;type:jsonb;not null" json:"config"`
	CredentialID  *int64            `gorm:"column:credential_id" json:"credentialId,omitempty"`
	Credential    *CredentialSecret `gorm:"foreignKey:CredentialID" json:"-"`
	Enabled       bool              `gorm:"column:enabled;not null" json:"enabled"`
	ReadOnly      bool              `gorm:"column:read_only;not null" json:"readOnly"`
	CreatedBy     *int64            `gorm:"column:created_by" json:"createdBy,omitempty"`
	CreatedAt     time.Time         `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt     time.Time         `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (DataSource) TableName() string {
	return "data_source"
}
