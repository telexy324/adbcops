package nacos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/resourcelimit"
)

const (
	defaultGroup        = "DEFAULT_GROUP"
	defaultTimeout      = 10 * time.Second
	maxTimeout          = 30 * time.Second
	defaultPageNo       = 1
	defaultPageSize     = 50
	maxPageSize         = 200
	defaultMaxServices  = 200
	defaultMaxInstances = 500
	maxBodyBytes        = 4 << 20
)

var (
	ErrForbidden          = errors.New("nacos access forbidden")
	ErrInvalidInput       = errors.New("invalid input")
	ErrUnsupportedSource  = errors.New("unsupported nacos data source")
	ErrDataSourceDisabled = errors.New("data source disabled")
	ErrDataSourceNotRead  = errors.New("nacos data source must be read-only")
	ErrScopeNotAllowed    = errors.New("nacos namespace or group is not allowed")
	ErrNacosTimeout       = errors.New("nacos query timeout")
	ErrDataSourceLimited  = errors.New("nacos data source concurrency limit exceeded")
)

type Repository interface {
	FindDataSourceByID(ctx context.Context, id int64) (*model.DataSource, error)
}

type SecretManager interface {
	Decrypt(value string) (string, error)
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Service struct {
	repository Repository
	secrets    SecretManager
	client     HTTPClient
	limiter    *resourcelimit.KeyedLimiter
}

type Config struct {
	BaseURL               string   `json:"baseUrl"`
	Namespace             string   `json:"namespace"`
	DefaultGroup          string   `json:"defaultGroup"`
	AllowedNamespaces     []string `json:"allowedNamespaces"`
	AllowedGroups         []string `json:"allowedGroups"`
	QueryTimeoutSeconds   int      `json:"queryTimeoutSeconds"`
	MaxServices           int      `json:"maxServices"`
	MaxInstances          int      `json:"maxInstances"`
	ServiceListPath       string   `json:"serviceListPath"`
	InstanceListPath      string   `json:"instanceListPath"`
	ConfigMetadataPath    string   `json:"configMetadataPath"`
	ConfigHistoryPath     string   `json:"configHistoryPath"`
	ClientConnectionsPath string   `json:"clientConnectionsPath"`
	ListenersPath         string   `json:"listenersPath"`
}

type credentialConfig struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	BearerToken string `json:"bearerToken"`
	AccessToken string `json:"accessToken"`
}

type QueryScope struct {
	Namespace string `json:"namespace,omitempty"`
	Group     string `json:"group,omitempty"`
}

type ListServicesInput struct {
	DataSourceID int64
	QueryScope
	PageNo   int
	PageSize int
}

type ListInstancesInput struct {
	DataSourceID int64
	QueryScope
	ServiceName string
	Clusters    string
	HealthyOnly *bool
}

type ConfigMetadataInput struct {
	DataSourceID int64
	QueryScope
	DataID string
}

type ConfigHistoryInput struct {
	DataSourceID int64
	QueryScope
	DataID string
	Limit  int
}

type ClientConnectionsInput struct {
	DataSourceID int64
	QueryScope
	ServiceName string
	PageNo      int
	PageSize    int
}

type ListenersInput struct {
	DataSourceID int64
	QueryScope
	DataID string
	Limit  int
}

type ServiceListResult struct {
	DataSourceID int64        `json:"dataSourceId"`
	Namespace    string       `json:"namespace"`
	Group        string       `json:"group"`
	PageNo       int          `json:"pageNo"`
	PageSize     int          `json:"pageSize"`
	Total        int          `json:"total"`
	Services     []NacosEntry `json:"services"`
}

type InstanceListResult struct {
	DataSourceID int64           `json:"dataSourceId"`
	Namespace    string          `json:"namespace"`
	Group        string          `json:"group"`
	ServiceName  string          `json:"serviceName"`
	Instances    []NacosInstance `json:"instances"`
}

type ConfigMetadataResult struct {
	DataSourceID int64             `json:"dataSourceId"`
	Namespace    string            `json:"namespace"`
	Group        string            `json:"group"`
	DataID       string            `json:"dataId"`
	Metadata     map[string]string `json:"metadata"`
}

type ConfigHistoryResult struct {
	DataSourceID int64             `json:"dataSourceId"`
	Namespace    string            `json:"namespace"`
	Group        string            `json:"group"`
	DataID       string            `json:"dataId"`
	Changes      []ConfigChange    `json:"changes"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type ClientConnectionsResult struct {
	DataSourceID int64        `json:"dataSourceId"`
	Namespace    string       `json:"namespace"`
	Group        string       `json:"group"`
	ServiceName  string       `json:"serviceName,omitempty"`
	PageNo       int          `json:"pageNo"`
	PageSize     int          `json:"pageSize"`
	Total        int          `json:"total"`
	Clients      []NacosEntry `json:"clients"`
}

type ListenersResult struct {
	DataSourceID int64        `json:"dataSourceId"`
	Namespace    string       `json:"namespace"`
	Group        string       `json:"group"`
	DataID       string       `json:"dataId,omitempty"`
	Listeners    []NacosEntry `json:"listeners"`
}

type NacosEntry struct {
	Name       string            `json:"name,omitempty"`
	ID         string            `json:"id,omitempty"`
	IP         string            `json:"ip,omitempty"`
	Port       int               `json:"port,omitempty"`
	Healthy    *bool             `json:"healthy,omitempty"`
	Enabled    *bool             `json:"enabled,omitempty"`
	Ephemeral  *bool             `json:"ephemeral,omitempty"`
	Cluster    string            `json:"cluster,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type NacosInstance struct {
	IP        string            `json:"ip"`
	Port      int               `json:"port"`
	Healthy   bool              `json:"healthy"`
	Enabled   bool              `json:"enabled"`
	Ephemeral bool              `json:"ephemeral"`
	Cluster   string            `json:"cluster,omitempty"`
	Weight    float64           `json:"weight,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type ConfigChange struct {
	ID           string            `json:"id,omitempty"`
	DataID       string            `json:"dataId,omitempty"`
	Group        string            `json:"group,omitempty"`
	Namespace    string            `json:"namespace,omitempty"`
	Operator     string            `json:"operator,omitempty"`
	SourceIP     string            `json:"sourceIp,omitempty"`
	Type         string            `json:"type,omitempty"`
	MD5          string            `json:"md5,omitempty"`
	LastModified string            `json:"lastModified,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type nacosPageResponse struct {
	Count          int               `json:"count"`
	TotalCount     int               `json:"totalCount"`
	PageNumber     int               `json:"pageNumber"`
	PageItems      []json.RawMessage `json:"pageItems"`
	Doms           []string          `json:"doms"`
	ServiceNames   []string          `json:"serviceNames"`
	Data           json.RawMessage   `json:"data"`
	Rows           []json.RawMessage `json:"rows"`
	Items          []json.RawMessage `json:"items"`
	List           []json.RawMessage `json:"list"`
	Clients        []json.RawMessage `json:"clients"`
	Subscribers    []json.RawMessage `json:"subscribers"`
	Configurations []json.RawMessage `json:"configurations"`
}

func NewService(repository Repository, secrets SecretManager, client HTTPClient) *Service {
	if client == nil {
		client = &http.Client{Timeout: maxTimeout}
	}
	return &Service{
		repository: repository,
		secrets:    secrets,
		client:     client,
		limiter:    resourcelimit.NewKeyedLimiter(4),
	}
}

func (s *Service) SetDataSourceLimiter(limiter *resourcelimit.KeyedLimiter) {
	s.limiter = limiter
}

func (s *Service) Test(ctx context.Context, actor *model.AppUser, dataSourceID int64) error {
	_, err := s.ListServices(ctx, actor, ListServicesInput{DataSourceID: dataSourceID, PageSize: 1})
	return err
}

func (s *Service) ListServices(ctx context.Context, actor *model.AppUser, input ListServicesInput) (*ServiceListResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	input.PageNo, input.PageSize = normalizePage(input.PageNo, input.PageSize)
	dataSource, cfg, credential, scope, err := s.load(ctx, input.DataSourceID, input.QueryScope)
	if err != nil {
		return nil, err
	}
	values := url.Values{}
	values.Set("pageNo", strconv.Itoa(input.PageNo))
	values.Set("pageSize", strconv.Itoa(input.PageSize))
	values.Set("namespaceId", scope.Namespace)
	values.Set("groupName", scope.Group)
	var decoded nacosPageResponse
	if err := s.getJSON(ctx, dataSource.ID, cfg, credential, cfg.ServiceListPath, values, &decoded); err != nil {
		return nil, err
	}
	services := decodeServiceEntries(decoded)
	services = limitEntries(services, cfg.MaxServices, defaultMaxServices)
	return &ServiceListResult{
		DataSourceID: dataSource.ID,
		Namespace:    scope.Namespace,
		Group:        scope.Group,
		PageNo:       input.PageNo,
		PageSize:     input.PageSize,
		Total:        firstPositive(decoded.Count, decoded.TotalCount, len(services)),
		Services:     services,
	}, nil
}

func (s *Service) ListInstances(ctx context.Context, actor *model.AppUser, input ListInstancesInput) (*InstanceListResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	input.ServiceName = strings.TrimSpace(input.ServiceName)
	if input.ServiceName == "" {
		return nil, ErrInvalidInput
	}
	dataSource, cfg, credential, scope, err := s.load(ctx, input.DataSourceID, input.QueryScope)
	if err != nil {
		return nil, err
	}
	values := url.Values{}
	values.Set("serviceName", input.ServiceName)
	values.Set("namespaceId", scope.Namespace)
	values.Set("groupName", scope.Group)
	if strings.TrimSpace(input.Clusters) != "" {
		values.Set("clusters", strings.TrimSpace(input.Clusters))
	}
	if input.HealthyOnly != nil {
		values.Set("healthyOnly", strconv.FormatBool(*input.HealthyOnly))
	}
	var raw map[string]json.RawMessage
	if err := s.getJSON(ctx, dataSource.ID, cfg, credential, cfg.InstanceListPath, values, &raw); err != nil {
		return nil, err
	}
	instances := decodeInstances(raw)
	if len(instances) > cfg.maxInstances() {
		instances = instances[:cfg.maxInstances()]
	}
	return &InstanceListResult{
		DataSourceID: dataSource.ID,
		Namespace:    scope.Namespace,
		Group:        scope.Group,
		ServiceName:  input.ServiceName,
		Instances:    instances,
	}, nil
}

func (s *Service) GetConfigMetadata(ctx context.Context, actor *model.AppUser, input ConfigMetadataInput) (*ConfigMetadataResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	input.DataID = strings.TrimSpace(input.DataID)
	if input.DataID == "" {
		return nil, ErrInvalidInput
	}
	dataSource, cfg, credential, scope, err := s.load(ctx, input.DataSourceID, input.QueryScope)
	if err != nil {
		return nil, err
	}
	values := configValues(scope, input.DataID)
	var raw map[string]interface{}
	if err := s.getJSON(ctx, dataSource.ID, cfg, credential, cfg.ConfigMetadataPath, values, &raw); err != nil {
		return nil, err
	}
	return &ConfigMetadataResult{
		DataSourceID: dataSource.ID,
		Namespace:    scope.Namespace,
		Group:        scope.Group,
		DataID:       input.DataID,
		Metadata:     sanitizeMetadata(raw),
	}, nil
}

func (s *Service) ListConfigChanges(ctx context.Context, actor *model.AppUser, input ConfigHistoryInput) (*ConfigHistoryResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	input.DataID = strings.TrimSpace(input.DataID)
	if input.DataID == "" {
		return nil, ErrInvalidInput
	}
	if input.Limit <= 0 || input.Limit > maxPageSize {
		input.Limit = defaultPageSize
	}
	dataSource, cfg, credential, scope, err := s.load(ctx, input.DataSourceID, input.QueryScope)
	if err != nil {
		return nil, err
	}
	values := configValues(scope, input.DataID)
	values.Set("pageNo", "1")
	values.Set("pageSize", strconv.Itoa(input.Limit))
	var decoded nacosPageResponse
	if err := s.getJSON(ctx, dataSource.ID, cfg, credential, cfg.ConfigHistoryPath, values, &decoded); err != nil {
		return nil, err
	}
	changes := decodeConfigChanges(decoded, input.Limit)
	return &ConfigHistoryResult{
		DataSourceID: dataSource.ID,
		Namespace:    scope.Namespace,
		Group:        scope.Group,
		DataID:       input.DataID,
		Changes:      changes,
	}, nil
}

func (s *Service) ListClientConnections(ctx context.Context, actor *model.AppUser, input ClientConnectionsInput) (*ClientConnectionsResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	input.PageNo, input.PageSize = normalizePage(input.PageNo, input.PageSize)
	dataSource, cfg, credential, scope, err := s.load(ctx, input.DataSourceID, input.QueryScope)
	if err != nil {
		return nil, err
	}
	values := url.Values{}
	values.Set("namespaceId", scope.Namespace)
	values.Set("groupName", scope.Group)
	values.Set("pageNo", strconv.Itoa(input.PageNo))
	values.Set("pageSize", strconv.Itoa(input.PageSize))
	if strings.TrimSpace(input.ServiceName) != "" {
		values.Set("serviceName", strings.TrimSpace(input.ServiceName))
	}
	var decoded nacosPageResponse
	if err := s.getJSON(ctx, dataSource.ID, cfg, credential, cfg.ClientConnectionsPath, values, &decoded); err != nil {
		return nil, err
	}
	clients := decodeGenericEntries(firstRawList(decoded.Clients, decoded.PageItems, decoded.Items, decoded.List))
	return &ClientConnectionsResult{
		DataSourceID: dataSource.ID,
		Namespace:    scope.Namespace,
		Group:        scope.Group,
		ServiceName:  strings.TrimSpace(input.ServiceName),
		PageNo:       input.PageNo,
		PageSize:     input.PageSize,
		Total:        firstPositive(decoded.Count, decoded.TotalCount, len(clients)),
		Clients:      clients,
	}, nil
}

func (s *Service) ListListeners(ctx context.Context, actor *model.AppUser, input ListenersInput) (*ListenersResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	if input.Limit <= 0 || input.Limit > maxPageSize {
		input.Limit = defaultPageSize
	}
	dataSource, cfg, credential, scope, err := s.load(ctx, input.DataSourceID, input.QueryScope)
	if err != nil {
		return nil, err
	}
	values := url.Values{}
	values.Set("namespaceId", scope.Namespace)
	values.Set("groupName", scope.Group)
	values.Set("pageSize", strconv.Itoa(input.Limit))
	if strings.TrimSpace(input.DataID) != "" {
		values.Set("dataId", strings.TrimSpace(input.DataID))
	}
	var decoded nacosPageResponse
	if err := s.getJSON(ctx, dataSource.ID, cfg, credential, cfg.ListenersPath, values, &decoded); err != nil {
		return nil, err
	}
	listeners := decodeGenericEntries(firstRawList(decoded.Subscribers, decoded.PageItems, decoded.Items, decoded.List))
	return &ListenersResult{
		DataSourceID: dataSource.ID,
		Namespace:    scope.Namespace,
		Group:        scope.Group,
		DataID:       strings.TrimSpace(input.DataID),
		Listeners:    listeners,
	}, nil
}

func (s *Service) load(ctx context.Context, dataSourceID int64, requested QueryScope) (*model.DataSource, Config, credentialConfig, QueryScope, error) {
	if dataSourceID <= 0 {
		return nil, Config{}, credentialConfig{}, QueryScope{}, ErrInvalidInput
	}
	dataSource, err := s.repository.FindDataSourceByID(ctx, dataSourceID)
	if err != nil {
		return nil, Config{}, credentialConfig{}, QueryScope{}, err
	}
	if !dataSource.Enabled {
		return nil, Config{}, credentialConfig{}, QueryScope{}, ErrDataSourceDisabled
	}
	if dataSource.SourceType != model.DataSourceTypeNacos {
		return nil, Config{}, credentialConfig{}, QueryScope{}, ErrUnsupportedSource
	}
	if !dataSource.ReadOnly {
		return nil, Config{}, credentialConfig{}, QueryScope{}, ErrDataSourceNotRead
	}
	cfg, err := parseConfig(dataSource.Config)
	if err != nil {
		return nil, Config{}, credentialConfig{}, QueryScope{}, err
	}
	scope, err := cfg.resolveScope(requested)
	if err != nil {
		return nil, Config{}, credentialConfig{}, QueryScope{}, err
	}
	credential, err := s.loadCredential(dataSource)
	if err != nil {
		return nil, Config{}, credentialConfig{}, QueryScope{}, err
	}
	return dataSource, cfg, credential, scope, nil
}

func parseConfig(raw []byte) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, ErrInvalidInput
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return Config{}, ErrInvalidInput
	}
	cfg.Namespace = strings.TrimSpace(cfg.Namespace)
	cfg.DefaultGroup = strings.TrimSpace(cfg.DefaultGroup)
	if cfg.DefaultGroup == "" {
		cfg.DefaultGroup = defaultGroup
	}
	cfg.AllowedNamespaces = normalizeList(cfg.AllowedNamespaces)
	cfg.AllowedGroups = normalizeList(cfg.AllowedGroups)
	if len(cfg.AllowedNamespaces) == 0 && cfg.Namespace != "" {
		cfg.AllowedNamespaces = []string{cfg.Namespace}
	}
	if len(cfg.AllowedGroups) == 0 {
		cfg.AllowedGroups = []string{cfg.DefaultGroup}
	}
	cfg.ServiceListPath = defaultPath(cfg.ServiceListPath, "/nacos/v1/ns/service/list")
	cfg.InstanceListPath = defaultPath(cfg.InstanceListPath, "/nacos/v1/ns/instance/list")
	cfg.ConfigMetadataPath = defaultPath(cfg.ConfigMetadataPath, "/nacos/v1/cs/configs/metadata")
	cfg.ConfigHistoryPath = defaultPath(cfg.ConfigHistoryPath, "/nacos/v1/cs/history")
	cfg.ClientConnectionsPath = defaultPath(cfg.ClientConnectionsPath, "/nacos/v1/ns/client/list")
	cfg.ListenersPath = defaultPath(cfg.ListenersPath, "/nacos/v1/cs/listeners")
	return cfg, nil
}

func (cfg Config) resolveScope(requested QueryScope) (QueryScope, error) {
	scope := QueryScope{
		Namespace: strings.TrimSpace(requested.Namespace),
		Group:     strings.TrimSpace(requested.Group),
	}
	if scope.Namespace == "" {
		scope.Namespace = cfg.Namespace
	}
	if scope.Group == "" {
		scope.Group = cfg.DefaultGroup
	}
	if !containsOrWildcard(cfg.AllowedNamespaces, scope.Namespace) || !containsOrWildcard(cfg.AllowedGroups, scope.Group) {
		return QueryScope{}, ErrScopeNotAllowed
	}
	return scope, nil
}

func (cfg Config) timeout() time.Duration {
	if cfg.QueryTimeoutSeconds <= 0 {
		return defaultTimeout
	}
	timeout := time.Duration(cfg.QueryTimeoutSeconds) * time.Second
	if timeout > maxTimeout {
		return maxTimeout
	}
	return timeout
}

func (cfg Config) maxInstances() int {
	if cfg.MaxInstances <= 0 {
		return defaultMaxInstances
	}
	if cfg.MaxInstances > defaultMaxInstances {
		return defaultMaxInstances
	}
	return cfg.MaxInstances
}

func (s *Service) loadCredential(dataSource *model.DataSource) (credentialConfig, error) {
	if dataSource.Credential == nil || dataSource.Credential.EncryptedPayload == "" || s.secrets == nil {
		return credentialConfig{}, nil
	}
	plaintext, err := s.secrets.Decrypt(dataSource.Credential.EncryptedPayload)
	if err != nil {
		return credentialConfig{}, fmt.Errorf("decrypt nacos credential: %w", err)
	}
	var credential credentialConfig
	if err := json.Unmarshal([]byte(plaintext), &credential); err != nil {
		return credentialConfig{}, ErrInvalidInput
	}
	return credential, nil
}

func (s *Service) getJSON(ctx context.Context, dataSourceID int64, cfg Config, credential credentialConfig, path string, values url.Values, output interface{}) error {
	release, err := s.limiter.Acquire(ctx, fmt.Sprintf("nacos:%d", dataSourceID))
	if err != nil {
		if errors.Is(err, resourcelimit.ErrLimitExceeded) {
			return ErrDataSourceLimited
		}
		return err
	}
	defer release()
	queryContext, cancel := context.WithTimeout(ctx, cfg.timeout())
	defer cancel()
	endpoint := cfg.BaseURL + path
	if encoded := values.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	request, err := http.NewRequestWithContext(queryContext, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create nacos request: %w", err)
	}
	applyCredential(request, credential)
	response, err := s.client.Do(request)
	if err != nil {
		if errors.Is(queryContext.Err(), context.DeadlineExceeded) {
			return ErrNacosTimeout
		}
		return fmt.Errorf("query nacos: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("read nacos response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("nacos returned status %d", response.StatusCode)
	}
	if err := json.Unmarshal(body, output); err != nil {
		return ErrInvalidInput
	}
	return nil
}

func applyCredential(request *http.Request, credential credentialConfig) {
	if strings.TrimSpace(credential.BearerToken) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(credential.BearerToken))
		return
	}
	if strings.TrimSpace(credential.AccessToken) != "" {
		query := request.URL.Query()
		query.Set("accessToken", strings.TrimSpace(credential.AccessToken))
		request.URL.RawQuery = query.Encode()
		return
	}
	if credential.Username != "" || credential.Password != "" {
		request.SetBasicAuth(credential.Username, credential.Password)
	}
}

func normalizePage(pageNo, pageSize int) (int, int) {
	if pageNo <= 0 {
		pageNo = defaultPageNo
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return pageNo, pageSize
}

func normalizeList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func containsOrWildcard(values []string, target string) bool {
	for _, value := range values {
		if value == "*" || value == target {
			return true
		}
	}
	return false
}

func defaultPath(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if !strings.HasPrefix(value, "/") {
		return "/" + value
	}
	return value
}

func configValues(scope QueryScope, dataID string) url.Values {
	values := url.Values{}
	values.Set("dataId", dataID)
	values.Set("group", scope.Group)
	values.Set("tenant", scope.Namespace)
	values.Set("namespaceId", scope.Namespace)
	return values
}

func decodeServiceEntries(decoded nacosPageResponse) []NacosEntry {
	names := append([]string{}, decoded.Doms...)
	names = append(names, decoded.ServiceNames...)
	entries := make([]NacosEntry, 0, len(names)+len(decoded.PageItems)+len(decoded.Items))
	for _, name := range names {
		if strings.TrimSpace(name) != "" {
			entries = append(entries, NacosEntry{Name: strings.TrimSpace(name)})
		}
	}
	for _, raw := range firstRawList(decoded.PageItems, decoded.Items, decoded.List) {
		entry := rawToEntry(raw)
		if entry.Name != "" || entry.ID != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

func decodeInstances(raw map[string]json.RawMessage) []NacosInstance {
	var rawHosts []json.RawMessage
	for _, key := range []string{"hosts", "instances", "list"} {
		if value, ok := raw[key]; ok {
			_ = json.Unmarshal(value, &rawHosts)
			break
		}
	}
	instances := make([]NacosInstance, 0, len(rawHosts))
	for _, item := range rawHosts {
		var decoded struct {
			IP          string            `json:"ip"`
			Port        int               `json:"port"`
			Healthy     bool              `json:"healthy"`
			Enabled     bool              `json:"enabled"`
			Ephemeral   bool              `json:"ephemeral"`
			ClusterName string            `json:"clusterName"`
			Cluster     string            `json:"cluster"`
			Weight      float64           `json:"weight"`
			Metadata    map[string]string `json:"metadata"`
		}
		if err := json.Unmarshal(item, &decoded); err != nil || decoded.IP == "" || decoded.Port <= 0 {
			continue
		}
		cluster := decoded.ClusterName
		if cluster == "" {
			cluster = decoded.Cluster
		}
		instances = append(instances, NacosInstance{
			IP:        decoded.IP,
			Port:      decoded.Port,
			Healthy:   decoded.Healthy,
			Enabled:   decoded.Enabled,
			Ephemeral: decoded.Ephemeral,
			Cluster:   cluster,
			Weight:    decoded.Weight,
			Metadata:  decoded.Metadata,
		})
	}
	return instances
}

func decodeConfigChanges(decoded nacosPageResponse, limit int) []ConfigChange {
	rawItems := firstRawList(decoded.PageItems, decoded.Items, decoded.List, decoded.Rows, decoded.Configurations)
	changes := make([]ConfigChange, 0, len(rawItems))
	for _, raw := range rawItems {
		var item map[string]interface{}
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}
		metadata := sanitizeMetadata(item)
		changes = append(changes, ConfigChange{
			ID:           metadata["id"],
			DataID:       firstString(metadata["dataId"], metadata["dataID"]),
			Group:        metadata["group"],
			Namespace:    firstString(metadata["tenant"], metadata["namespaceId"], metadata["namespace"]),
			Operator:     firstString(metadata["operator"], metadata["srcUser"]),
			SourceIP:     firstString(metadata["sourceIp"], metadata["srcIp"]),
			Type:         firstString(metadata["type"], metadata["opType"]),
			MD5:          firstString(metadata["md5"], metadata["md5Value"]),
			LastModified: firstString(metadata["lastModified"], metadata["lastModifiedTime"], metadata["gmtModified"]),
			Metadata:     metadata,
		})
	}
	if len(changes) > limit {
		changes = changes[:limit]
	}
	return changes
}

func decodeGenericEntries(rawItems []json.RawMessage) []NacosEntry {
	entries := make([]NacosEntry, 0, len(rawItems))
	for _, raw := range rawItems {
		entry := rawToEntry(raw)
		if entry.Name != "" || entry.ID != "" || entry.IP != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

func rawToEntry(raw json.RawMessage) NacosEntry {
	var item map[string]interface{}
	if err := json.Unmarshal(raw, &item); err != nil {
		return NacosEntry{}
	}
	metadata := sanitizeMetadata(item)
	name := firstString(metadata["serviceName"], metadata["name"], metadata["dataId"], metadata["dataID"])
	cluster := firstString(metadata["clusterName"], metadata["cluster"])
	entry := NacosEntry{
		Name:       name,
		ID:         firstString(metadata["id"], metadata["clientId"], metadata["connectionId"]),
		IP:         firstString(metadata["ip"], metadata["clientIp"], metadata["remoteIp"]),
		Port:       toInt(item["port"]),
		Cluster:    cluster,
		Healthy:    toBoolPtr(item["healthy"]),
		Enabled:    toBoolPtr(item["enabled"]),
		Ephemeral:  toBoolPtr(item["ephemeral"]),
		Metadata:   stringMap(item["metadata"]),
		Attributes: metadata,
	}
	return entry
}

func sanitizeMetadata(raw map[string]interface{}) map[string]string {
	result := make(map[string]string)
	for key, value := range raw {
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey == "" || isSensitivePayloadKey(normalizedKey) {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				result[normalizedKey] = strings.TrimSpace(typed)
			}
		case float64:
			result[normalizedKey] = strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			result[normalizedKey] = strconv.FormatBool(typed)
		}
	}
	return result
}

func isSensitivePayloadKey(key string) bool {
	switch strings.ToLower(key) {
	case "content", "value", "config", "encrypteddata", "encrypted_data":
		return true
	default:
		return false
	}
}

func stringMap(value interface{}) map[string]string {
	raw, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	return sanitizeMetadata(raw)
}

func limitEntries(entries []NacosEntry, configuredMax, fallback int) []NacosEntry {
	limit := configuredMax
	if limit <= 0 {
		limit = fallback
	}
	if limit > fallback {
		limit = fallback
	}
	if len(entries) > limit {
		return entries[:limit]
	}
	return entries
}

func firstRawList(lists ...[]json.RawMessage) []json.RawMessage {
	for _, list := range lists {
		if len(list) > 0 {
			return list
		}
	}
	return nil
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func toInt(value interface{}) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
}

func toBoolPtr(value interface{}) *bool {
	switch typed := value.(type) {
	case bool:
		return &typed
	case string:
		parsed, err := strconv.ParseBool(typed)
		if err == nil {
			return &parsed
		}
	}
	return nil
}
