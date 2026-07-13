package redis

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/resourcelimit"
)

const (
	ModeStandalone = "standalone"
	ModeSentinel   = "sentinel"
	ModeCluster    = "cluster"

	defaultTimeout        = 5 * time.Second
	maxTimeout            = 20 * time.Second
	defaultSlowLogLimit   = 20
	maxSlowLogLimit       = 200
	defaultScanIterations = 20
	maxScanIterations     = 100
	defaultScanKeys       = 500
	maxScanKeys           = 2000
)

var (
	ErrForbidden          = errors.New("redis access forbidden")
	ErrInvalidInput       = errors.New("invalid input")
	ErrUnsupportedSource  = errors.New("unsupported redis data source")
	ErrDataSourceDisabled = errors.New("data source disabled")
	ErrDataSourceNotRead  = errors.New("redis data source must be read-only")
	ErrCommandNotAllowed  = errors.New("redis command is not allowed")
	ErrRedisTimeout       = errors.New("redis query timeout")
	ErrDataSourceLimited  = errors.New("redis data source concurrency limit exceeded")
)

type Repository interface {
	FindDataSourceByID(ctx context.Context, id int64) (*model.DataSource, error)
}

type SecretManager interface {
	Decrypt(value string) (string, error)
}

type Runner interface {
	Run(ctx context.Context, endpoint string, credential Credential, command Command) (Reply, error)
}

type Service struct {
	repository Repository
	secrets    SecretManager
	runner     Runner
	limiter    *resourcelimit.KeyedLimiter
}

type Config struct {
	Mode              string   `json:"mode"`
	Endpoints         []string `json:"endpoints"`
	MasterName        string   `json:"masterName"`
	DB                int      `json:"db"`
	UseTLS            bool     `json:"useTls"`
	QueryTimeoutSec   int      `json:"queryTimeoutSeconds"`
	MaxScanIterations int      `json:"maxScanIterations"`
	MaxScanKeys       int      `json:"maxScanKeys"`
}

type Credential struct {
	Username string `json:"username"`
	Password string `json:"password"`
	DB       int    `json:"db"`
	UseTLS   bool   `json:"useTls"`
}

type Command struct {
	Name string
	Args []string
}

type Reply interface{}

type QueryInput struct {
	DataSourceID int64
}

type InfoInput struct {
	DataSourceID int64
	Sections     []string
}

type SlowLogInput struct {
	DataSourceID int64
	Limit        int
}

type ScanInput struct {
	DataSourceID  int64
	Match         string
	Count         int
	MaxIterations int
	MaxKeys       int
}

type InfoResult struct {
	DataSourceID int64                        `json:"dataSourceId"`
	Mode         string                       `json:"mode"`
	Nodes        []NodeInfo                   `json:"nodes"`
	Sections     map[string]map[string]string `json:"sections"`
	Partial      bool                         `json:"partial"`
}

type NodeInfo struct {
	Endpoint string `json:"endpoint"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

type SlowLogItem struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp,omitempty"`
	Duration  string `json:"duration,omitempty"`
	Command   string `json:"command"`
	Source    string `json:"source"`
}

type SlowLogResult struct {
	DataSourceID int64         `json:"dataSourceId"`
	Items        []SlowLogItem `json:"items"`
	Partial      bool          `json:"partial"`
}

type MemoryStatsResult struct {
	DataSourceID int64                        `json:"dataSourceId"`
	Nodes        []NodeInfo                   `json:"nodes"`
	Stats        map[string]map[string]string `json:"stats"`
	Partial      bool                         `json:"partial"`
}

type ClientSummaryResult struct {
	DataSourceID int64           `json:"dataSourceId"`
	Nodes        []NodeInfo      `json:"nodes"`
	Summary      []ClientSummary `json:"summary"`
	Partial      bool            `json:"partial"`
}

type ClientSummary struct {
	Source string              `json:"source"`
	Count  int                 `json:"count"`
	ByCmd  map[string]int      `json:"byCmd,omitempty"`
	ByDB   map[string]int      `json:"byDb,omitempty"`
	Fields []map[string]string `json:"fields,omitempty"`
}

type ReplicationResult struct {
	DataSourceID int64                  `json:"dataSourceId"`
	Nodes        []NodeInfo             `json:"nodes"`
	Roles        map[string]interface{} `json:"roles"`
	Partial      bool                   `json:"partial"`
}

type ClusterResult struct {
	DataSourceID int64                        `json:"dataSourceId"`
	Nodes        []NodeInfo                   `json:"nodes"`
	Info         map[string]map[string]string `json:"info"`
	NodeSummary  map[string][]ClusterNode     `json:"nodeSummary"`
	Partial      bool                         `json:"partial"`
}

type ClusterNode struct {
	ID       string `json:"id,omitempty"`
	Address  string `json:"address,omitempty"`
	Flags    string `json:"flags,omitempty"`
	MasterID string `json:"masterId,omitempty"`
	Slots    string `json:"slots,omitempty"`
}

type SentinelResult struct {
	DataSourceID int64                          `json:"dataSourceId"`
	Nodes        []NodeInfo                     `json:"nodes"`
	Masters      map[string][]map[string]string `json:"masters,omitempty"`
	Replicas     map[string][]map[string]string `json:"replicas,omitempty"`
	Partial      bool                           `json:"partial"`
}

type LatencyResult struct {
	DataSourceID int64                          `json:"dataSourceId"`
	Nodes        []NodeInfo                     `json:"nodes"`
	Latest       map[string][]map[string]string `json:"latest"`
	Partial      bool                           `json:"partial"`
}

type ScanSummaryResult struct {
	DataSourceID    int64          `json:"dataSourceId"`
	Nodes           []NodeInfo     `json:"nodes"`
	ScannedKeys     int            `json:"scannedKeys"`
	Iterations      int            `json:"iterations"`
	PrefixHistogram map[string]int `json:"prefixHistogram"`
	Truncated       bool           `json:"truncated"`
	Partial         bool           `json:"partial"`
}

func NewService(repository Repository, secrets SecretManager, runner Runner) *Service {
	if runner == nil {
		runner = TCPRunner{}
	}
	return &Service{
		repository: repository,
		secrets:    secrets,
		runner:     runner,
		limiter:    resourcelimit.NewKeyedLimiter(4),
	}
}

func (s *Service) SetDataSourceLimiter(limiter *resourcelimit.KeyedLimiter) {
	s.limiter = limiter
}

func (s *Service) RunAllowedCommand(ctx context.Context, actor *model.AppUser, dataSourceID int64, command Command) (Reply, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	if err := validateCommand(command); err != nil {
		return nil, err
	}
	dataSource, cfg, credential, err := s.load(ctx, dataSourceID)
	if err != nil {
		return nil, err
	}
	return s.run(ctx, dataSource.ID, cfg.primaryEndpoint(), credential, command)
}

func (s *Service) Test(ctx context.Context, actor *model.AppUser, dataSourceID int64) error {
	_, err := s.RunAllowedCommand(ctx, actor, dataSourceID, Command{Name: "PING"})
	return err
}

func (s *Service) Info(ctx context.Context, actor *model.AppUser, input InfoInput) (*InfoResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, cfg, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	result := &InfoResult{DataSourceID: dataSource.ID, Mode: cfg.Mode, Sections: map[string]map[string]string{}}
	sections := input.Sections
	if len(sections) == 0 {
		sections = []string{"default"}
	}
	for _, endpoint := range cfg.queryEndpoints() {
		combined := map[string]string{}
		for _, section := range sections {
			reply, err := s.run(ctx, dataSource.ID, endpoint, credential, Command{Name: "INFO", Args: []string{strings.TrimSpace(section)}})
			if err != nil {
				result.Partial = true
				result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: false, Error: err.Error()})
				continue
			}
			mergeMap(combined, parseInfo(toString(reply)))
		}
		result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: true})
		result.Sections[endpoint] = combined
	}
	return result, nil
}

func (s *Service) SlowLog(ctx context.Context, actor *model.AppUser, input SlowLogInput) (*SlowLogResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	if input.Limit <= 0 || input.Limit > maxSlowLogLimit {
		input.Limit = defaultSlowLogLimit
	}
	dataSource, cfg, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	result := &SlowLogResult{DataSourceID: dataSource.ID}
	for _, endpoint := range cfg.queryEndpoints() {
		reply, err := s.run(ctx, dataSource.ID, endpoint, credential, Command{Name: "SLOWLOG", Args: []string{"GET", strconv.Itoa(input.Limit)}})
		if err != nil {
			result.Partial = true
			continue
		}
		result.Items = append(result.Items, decodeSlowLog(reply, endpoint)...)
	}
	if len(result.Items) > input.Limit {
		result.Items = result.Items[:input.Limit]
	}
	return result, nil
}

func (s *Service) MemoryStats(ctx context.Context, actor *model.AppUser, input QueryInput) (*MemoryStatsResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, cfg, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	result := &MemoryStatsResult{DataSourceID: dataSource.ID, Stats: map[string]map[string]string{}}
	for _, endpoint := range cfg.queryEndpoints() {
		reply, err := s.run(ctx, dataSource.ID, endpoint, credential, Command{Name: "MEMORY", Args: []string{"STATS"}})
		if err != nil {
			result.Partial = true
			result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: false, Error: err.Error()})
			continue
		}
		result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: true})
		result.Stats[endpoint] = flattenPairs(reply)
	}
	return result, nil
}

func (s *Service) ClientListSummary(ctx context.Context, actor *model.AppUser, input QueryInput) (*ClientSummaryResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, cfg, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	result := &ClientSummaryResult{DataSourceID: dataSource.ID}
	for _, endpoint := range cfg.queryEndpoints() {
		reply, err := s.run(ctx, dataSource.ID, endpoint, credential, Command{Name: "CLIENT", Args: []string{"LIST"}})
		if err != nil {
			result.Partial = true
			result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: false, Error: err.Error()})
			continue
		}
		result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: true})
		result.Summary = append(result.Summary, summarizeClients(endpoint, toString(reply)))
	}
	return result, nil
}

func (s *Service) Replication(ctx context.Context, actor *model.AppUser, input QueryInput) (*ReplicationResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, cfg, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	result := &ReplicationResult{DataSourceID: dataSource.ID, Roles: map[string]interface{}{}}
	for _, endpoint := range cfg.queryEndpoints() {
		reply, err := s.run(ctx, dataSource.ID, endpoint, credential, Command{Name: "ROLE"})
		if err != nil {
			result.Partial = true
			result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: false, Error: err.Error()})
			continue
		}
		result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: true})
		result.Roles[endpoint] = reply
	}
	return result, nil
}

func (s *Service) ClusterState(ctx context.Context, actor *model.AppUser, input QueryInput) (*ClusterResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, cfg, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	result := &ClusterResult{DataSourceID: dataSource.ID, Info: map[string]map[string]string{}, NodeSummary: map[string][]ClusterNode{}}
	for _, endpoint := range cfg.queryEndpoints() {
		infoReply, infoErr := s.run(ctx, dataSource.ID, endpoint, credential, Command{Name: "CLUSTER", Args: []string{"INFO"}})
		nodesReply, nodesErr := s.run(ctx, dataSource.ID, endpoint, credential, Command{Name: "CLUSTER", Args: []string{"NODES"}})
		if infoErr != nil || nodesErr != nil {
			result.Partial = true
			errText := firstError(infoErr, nodesErr)
			result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: false, Error: errText})
			continue
		}
		result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: true})
		result.Info[endpoint] = parseInfo(toString(infoReply))
		result.NodeSummary[endpoint] = parseClusterNodes(toString(nodesReply))
	}
	return result, nil
}

func (s *Service) SentinelState(ctx context.Context, actor *model.AppUser, input QueryInput) (*SentinelResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, cfg, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	result := &SentinelResult{DataSourceID: dataSource.ID, Masters: map[string][]map[string]string{}, Replicas: map[string][]map[string]string{}}
	for _, endpoint := range cfg.queryEndpoints() {
		masters, mastersErr := s.run(ctx, dataSource.ID, endpoint, credential, Command{Name: "SENTINEL", Args: []string{"MASTERS"}})
		replicas, replicasErr := s.run(ctx, dataSource.ID, endpoint, credential, Command{Name: "SENTINEL", Args: []string{"REPLICAS", cfg.MasterName}})
		if mastersErr != nil || replicasErr != nil {
			result.Partial = true
			result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: false, Error: firstError(mastersErr, replicasErr)})
			continue
		}
		result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: true})
		result.Masters[endpoint] = decodeArrayMaps(masters)
		result.Replicas[endpoint] = decodeArrayMaps(replicas)
	}
	return result, nil
}

func (s *Service) LatencyLatest(ctx context.Context, actor *model.AppUser, input QueryInput) (*LatencyResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, cfg, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	result := &LatencyResult{DataSourceID: dataSource.ID, Latest: map[string][]map[string]string{}}
	for _, endpoint := range cfg.queryEndpoints() {
		reply, err := s.run(ctx, dataSource.ID, endpoint, credential, Command{Name: "LATENCY", Args: []string{"LATEST"}})
		if err != nil {
			result.Partial = true
			result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: false, Error: err.Error()})
			continue
		}
		result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: true})
		result.Latest[endpoint] = decodeArrayMaps(reply)
	}
	return result, nil
}

func (s *Service) ScanSummary(ctx context.Context, actor *model.AppUser, input ScanInput) (*ScanSummaryResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	dataSource, cfg, credential, err := s.load(ctx, input.DataSourceID)
	if err != nil {
		return nil, err
	}
	maxIterations := normalizeLimit(input.MaxIterations, cfg.MaxScanIterations, defaultScanIterations, maxScanIterations)
	maxKeys := normalizeLimit(input.MaxKeys, cfg.MaxScanKeys, defaultScanKeys, maxScanKeys)
	count := input.Count
	if count <= 0 || count > 500 {
		count = 100
	}
	result := &ScanSummaryResult{DataSourceID: dataSource.ID, PrefixHistogram: map[string]int{}}
	for _, endpoint := range cfg.queryEndpoints() {
		cursor := "0"
		for i := 0; i < maxIterations; i++ {
			args := []string{cursor, "COUNT", strconv.Itoa(count)}
			if strings.TrimSpace(input.Match) != "" {
				args = append(args, "MATCH", strings.TrimSpace(input.Match))
			}
			reply, err := s.run(ctx, dataSource.ID, endpoint, credential, Command{Name: "SCAN", Args: args})
			if err != nil {
				result.Partial = true
				result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: false, Error: err.Error()})
				break
			}
			next, keys := decodeScan(reply)
			result.Iterations++
			for _, key := range keys {
				result.ScannedKeys++
				result.PrefixHistogram[keyPrefix(key)]++
				if result.ScannedKeys >= maxKeys {
					result.Truncated = true
					return result, nil
				}
			}
			cursor = next
			if cursor == "0" {
				break
			}
		}
		result.Nodes = append(result.Nodes, NodeInfo{Endpoint: endpoint, OK: true})
	}
	return result, nil
}

func (s *Service) load(ctx context.Context, dataSourceID int64) (*model.DataSource, Config, Credential, error) {
	if dataSourceID <= 0 {
		return nil, Config{}, Credential{}, ErrInvalidInput
	}
	dataSource, err := s.repository.FindDataSourceByID(ctx, dataSourceID)
	if err != nil {
		return nil, Config{}, Credential{}, err
	}
	if !dataSource.Enabled {
		return nil, Config{}, Credential{}, ErrDataSourceDisabled
	}
	if dataSource.SourceType != model.DataSourceTypeRedis {
		return nil, Config{}, Credential{}, ErrUnsupportedSource
	}
	if !dataSource.ReadOnly {
		return nil, Config{}, Credential{}, ErrDataSourceNotRead
	}
	cfg, err := parseConfig(dataSource.Config)
	if err != nil {
		return nil, Config{}, Credential{}, err
	}
	credential, err := s.loadCredential(dataSource)
	if err != nil {
		return nil, Config{}, Credential{}, err
	}
	if credential.DB == 0 {
		credential.DB = cfg.DB
	}
	credential.UseTLS = credential.UseTLS || cfg.UseTLS
	return dataSource, cfg, credential, nil
}

func parseConfig(raw []byte) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, ErrInvalidInput
	}
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	if cfg.Mode == "" {
		cfg.Mode = ModeStandalone
	}
	switch cfg.Mode {
	case ModeStandalone, ModeSentinel, ModeCluster:
	default:
		return Config{}, ErrInvalidInput
	}
	cfg.Endpoints = normalizeList(cfg.Endpoints)
	if len(cfg.Endpoints) == 0 {
		return Config{}, ErrInvalidInput
	}
	cfg.MasterName = strings.TrimSpace(cfg.MasterName)
	if cfg.Mode == ModeSentinel && cfg.MasterName == "" {
		return Config{}, ErrInvalidInput
	}
	return cfg, nil
}

func (cfg Config) timeout() time.Duration {
	if cfg.QueryTimeoutSec <= 0 {
		return defaultTimeout
	}
	timeout := time.Duration(cfg.QueryTimeoutSec) * time.Second
	if timeout > maxTimeout {
		return maxTimeout
	}
	return timeout
}

func (cfg Config) primaryEndpoint() string {
	return cfg.Endpoints[0]
}

func (cfg Config) queryEndpoints() []string {
	if cfg.Mode == ModeStandalone || cfg.Mode == ModeSentinel {
		return []string{cfg.primaryEndpoint()}
	}
	return cfg.Endpoints
}

func (s *Service) loadCredential(dataSource *model.DataSource) (Credential, error) {
	if dataSource.Credential == nil || dataSource.Credential.EncryptedPayload == "" || s.secrets == nil {
		return Credential{}, nil
	}
	plaintext, err := s.secrets.Decrypt(dataSource.Credential.EncryptedPayload)
	if err != nil {
		return Credential{}, fmt.Errorf("decrypt redis credential: %w", err)
	}
	var credential Credential
	if err := json.Unmarshal([]byte(plaintext), &credential); err != nil {
		return Credential{}, ErrInvalidInput
	}
	return credential, nil
}

func (s *Service) run(ctx context.Context, dataSourceID int64, endpoint string, credential Credential, command Command) (Reply, error) {
	if err := validateCommand(command); err != nil {
		return nil, err
	}
	release, err := s.limiter.Acquire(ctx, fmt.Sprintf("redis:%d", dataSourceID))
	if err != nil {
		if errors.Is(err, resourcelimit.ErrLimitExceeded) {
			return nil, ErrDataSourceLimited
		}
		return nil, err
	}
	defer release()
	queryContext, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	reply, err := s.runner.Run(queryContext, endpoint, credential, command)
	if err != nil {
		if errors.Is(queryContext.Err(), context.DeadlineExceeded) {
			return nil, ErrRedisTimeout
		}
		return nil, err
	}
	return reply, nil
}

func validateCommand(command Command) error {
	name := strings.ToUpper(strings.TrimSpace(command.Name))
	args := upperArgs(command.Args)
	switch name {
	case "PING", "ROLE", "DBSIZE":
		if len(args) == 0 {
			return nil
		}
	case "INFO":
		if len(args) <= 1 {
			return nil
		}
	case "SLOWLOG":
		if len(args) >= 1 && args[0] == "GET" && len(args) <= 2 {
			return nil
		}
	case "LATENCY":
		if len(args) == 1 && args[0] == "LATEST" {
			return nil
		}
	case "MEMORY":
		if len(args) == 1 && args[0] == "STATS" {
			return nil
		}
	case "CLIENT":
		if len(args) == 1 && args[0] == "LIST" {
			return nil
		}
	case "CLUSTER":
		if len(args) == 1 && (args[0] == "INFO" || args[0] == "NODES") {
			return nil
		}
	case "SENTINEL":
		if len(args) >= 1 && (args[0] == "MASTERS" || args[0] == "REPLICAS") {
			return nil
		}
	case "SCAN":
		return validateScanArgs(args)
	}
	return ErrCommandNotAllowed
}

func validateScanArgs(args []string) error {
	if len(args) == 0 || len(args)%2 == 0 {
		return ErrCommandNotAllowed
	}
	for i := 1; i < len(args); i += 2 {
		switch args[i] {
		case "MATCH", "COUNT":
		default:
			return ErrCommandNotAllowed
		}
	}
	return nil
}

type TCPRunner struct{}

func (TCPRunner) Run(ctx context.Context, endpoint string, credential Credential, command Command) (Reply, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", endpoint)
	if err != nil {
		return nil, err
	}
	if credential.UseTLS {
		tlsConn := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, err
		}
		conn = tlsConn
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	reader := bufio.NewReader(conn)
	if credential.Password != "" {
		authArgs := []string{credential.Password}
		if credential.Username != "" {
			authArgs = []string{credential.Username, credential.Password}
		}
		if err := writeCommand(conn, Command{Name: "AUTH", Args: authArgs}); err != nil {
			return nil, err
		}
		if _, err := readRESP(reader); err != nil {
			return nil, err
		}
	}
	if credential.DB > 0 {
		if err := writeCommand(conn, Command{Name: "SELECT", Args: []string{strconv.Itoa(credential.DB)}}); err != nil {
			return nil, err
		}
		if _, err := readRESP(reader); err != nil {
			return nil, err
		}
	}
	if err := writeCommand(conn, command); err != nil {
		return nil, err
	}
	return readRESP(reader)
}

func writeCommand(w io.Writer, command Command) error {
	parts := append([]string{strings.ToUpper(strings.TrimSpace(command.Name))}, command.Args...)
	if _, err := fmt.Fprintf(w, "*%d\r\n", len(parts)); err != nil {
		return err
	}
	for _, part := range parts {
		if _, err := fmt.Fprintf(w, "$%d\r\n%s\r\n", len(part), part); err != nil {
			return err
		}
	}
	return nil
}

func readRESP(reader *bufio.Reader) (Reply, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	switch prefix {
	case '+':
		return readLine(reader)
	case '-':
		line, _ := readLine(reader)
		return nil, errors.New(line)
	case ':':
		line, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		value, err := strconv.ParseInt(line, 10, 64)
		return value, err
	case '$':
		line, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if size < 0 {
			return "", nil
		}
		buffer := make([]byte, size+2)
		if _, err := io.ReadFull(reader, buffer); err != nil {
			return nil, err
		}
		return string(buffer[:size]), nil
	case '*':
		line, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		count, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		items := make([]Reply, 0, count)
		for i := 0; i < count; i++ {
			item, err := readRESP(reader)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	default:
		return nil, ErrInvalidInput
	}
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

func upperArgs(args []string) []string {
	result := make([]string, len(args))
	for i, arg := range args {
		result[i] = strings.ToUpper(strings.TrimSpace(arg))
	}
	return result
}

func normalizeList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func normalizeLimit(input, configured, fallback, max int) int {
	limit := input
	if limit <= 0 {
		limit = configured
	}
	if limit <= 0 {
		limit = fallback
	}
	if limit > max {
		limit = max
	}
	return limit
}

func toString(reply Reply) string {
	switch typed := reply.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func parseInfo(raw string) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			result[key] = value
		}
	}
	return result
}

func mergeMap(target, source map[string]string) {
	for key, value := range source {
		target[key] = value
	}
}

func flattenPairs(reply Reply) map[string]string {
	items, ok := reply.([]Reply)
	if !ok {
		return map[string]string{"raw": toString(reply)}
	}
	result := map[string]string{}
	for i := 0; i+1 < len(items); i += 2 {
		result[toString(items[i])] = toString(items[i+1])
	}
	return result
}

func summarizeClients(source, raw string) ClientSummary {
	summary := ClientSummary{Source: source, ByCmd: map[string]int{}, ByDB: map[string]int{}}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := map[string]string{}
		for _, token := range strings.Fields(line) {
			key, value, ok := strings.Cut(token, "=")
			if ok {
				fields[key] = redactClientField(key, value)
			}
		}
		summary.Count++
		if cmd := fields["cmd"]; cmd != "" {
			summary.ByCmd[cmd]++
		}
		if db := fields["db"]; db != "" {
			summary.ByDB[db]++
		}
		summary.Fields = append(summary.Fields, fields)
	}
	return summary
}

func redactClientField(key, value string) string {
	switch strings.ToLower(key) {
	case "addr", "laddr", "name", "user":
		if value == "" {
			return ""
		}
		return "***"
	default:
		return value
	}
}

func decodeSlowLog(reply Reply, source string) []SlowLogItem {
	rows, ok := reply.([]Reply)
	if !ok {
		return nil
	}
	items := make([]SlowLogItem, 0, len(rows))
	for _, row := range rows {
		cols, ok := row.([]Reply)
		if !ok || len(cols) < 4 {
			continue
		}
		items = append(items, SlowLogItem{
			ID:        toString(cols[0]),
			Timestamp: toString(cols[1]),
			Duration:  toString(cols[2]),
			Command:   redactCommand(cols[3]),
			Source:    source,
		})
	}
	return items
}

func redactCommand(reply Reply) string {
	args, ok := reply.([]Reply)
	if !ok || len(args) == 0 {
		return "[redacted]"
	}
	command := strings.ToUpper(toString(args[0]))
	if len(args) == 1 {
		return command
	}
	return command + " [args redacted]"
}

func decodeScan(reply Reply) (string, []string) {
	items, ok := reply.([]Reply)
	if !ok || len(items) != 2 {
		return "0", nil
	}
	cursor := toString(items[0])
	rawKeys, ok := items[1].([]Reply)
	if !ok {
		return cursor, nil
	}
	keys := make([]string, 0, len(rawKeys))
	for _, raw := range rawKeys {
		keys = append(keys, toString(raw))
	}
	return cursor, keys
}

func keyPrefix(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "(empty)"
	}
	for _, sep := range []string{":", "/", ".", "-"} {
		if index := strings.Index(key, sep); index > 0 {
			return key[:index] + sep + "*"
		}
	}
	if len(key) > 12 {
		return key[:12] + "*"
	}
	return key
}

func parseClusterNodes(raw string) []ClusterNode {
	lines := strings.Split(raw, "\n")
	nodes := make([]ClusterNode, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		node := ClusterNode{ID: fields[0], Address: fields[1], Flags: fields[2]}
		if len(fields) > 3 {
			node.MasterID = fields[3]
		}
		if len(fields) > 8 {
			node.Slots = strings.Join(fields[8:], " ")
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func decodeArrayMaps(reply Reply) []map[string]string {
	rows, ok := reply.([]Reply)
	if !ok {
		return nil
	}
	result := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, flattenPairs(row))
	}
	return result
}

func firstError(errors ...error) string {
	for _, err := range errors {
		if err != nil {
			return err.Error()
		}
	}
	return ""
}
