package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	linuxhostsvc "aiops-platform/backend/internal/linuxhost"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type LinuxHostHandler struct {
	service    *linuxhostsvc.Service
	importer   *linuxhostsvc.BatchImporter
	batchTests *linuxhostsvc.BatchTestManager
}

func (h *LinuxHostHandler) WithBatchTests(manager *linuxhostsvc.BatchTestManager) *LinuxHostHandler {
	h.batchTests = manager
	return h
}

func (h *LinuxHostHandler) StartBatchTest(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	if h.batchTests == nil {
		failure(c, http.StatusServiceUnavailable, 50322, "batch test service is unavailable")
		return
	}
	var request linuxhostsvc.BatchTestSelection
	if c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	job, err := h.batchTests.Start(c.Request.Context(), actor, request)
	if handleLinuxBatchError(c, err) {
		return
	}
	success(c, job)
}
func (h *LinuxHostHandler) GetBatchTest(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	if h.batchTests == nil {
		failure(c, http.StatusServiceUnavailable, 50322, "batch test service is unavailable")
		return
	}
	job, err := h.batchTests.Get(actor, c.Param("jobId"))
	if handleLinuxBatchError(c, err) {
		return
	}
	success(c, job)
}
func (h *LinuxHostHandler) CancelBatchTest(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	if h.batchTests == nil {
		failure(c, http.StatusServiceUnavailable, 50322, "batch test service is unavailable")
		return
	}
	job, err := h.batchTests.Cancel(c.Request.Context(), actor, c.Param("jobId"))
	if handleLinuxBatchError(c, err) {
		return
	}
	success(c, job)
}
func (h *LinuxHostHandler) DownloadBatchTest(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	if h.batchTests == nil {
		failure(c, http.StatusServiceUnavailable, 50322, "batch test service is unavailable")
		return
	}
	payload, contentType, err := h.batchTests.Export(actor, c.Param("jobId"), c.Query("format"))
	if handleLinuxBatchError(c, err) {
		return
	}
	extension := "json"
	if strings.HasPrefix(contentType, "text/csv") {
		extension = "csv"
	}
	c.Header("Content-Disposition", `attachment; filename="linux-batch-test-`+c.Param("jobId")+`.`+extension+`"`)
	c.Data(http.StatusOK, contentType, payload)
}
func handleLinuxBatchError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	recordFailureError(c, err, "linux batch test failed")
	switch {
	case errors.Is(err, linuxhostsvc.ErrBatchTooManyHosts):
		failure(c, http.StatusBadRequest, 40001, err.Error())
	case errors.Is(err, linuxhostsvc.ErrBatchEmpty), errors.Is(err, linuxhostsvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, err.Error())
	case errors.Is(err, linuxhostsvc.ErrBatchNotFound):
		failure(c, http.StatusNotFound, 40401, err.Error())
	case errors.Is(err, linuxhostsvc.ErrBatchRunning):
		failure(c, http.StatusConflict, 40922, err.Error())
	default:
		failure(c, http.StatusInternalServerError, 50097, "linux batch test failed")
	}
	return true
}

func (h *LinuxHostHandler) WithBatchImporter(importer *linuxhostsvc.BatchImporter) *LinuxHostHandler {
	h.importer = importer
	return h
}

func (h *LinuxHostHandler) PreviewImport(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	if h.importer == nil {
		failure(c, http.StatusServiceUnavailable, 50321, "linux host importer is unavailable")
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	opened, err := file.Open()
	if err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	defer opened.Close()
	mapping := map[string]string{}
	if raw := c.PostForm("columnMapping"); raw != "" && json.Unmarshal([]byte(raw), &mapping) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid column mapping")
		return
	}
	result, err := h.importer.Preview(c.Request.Context(), actor, linuxhostsvc.ImportFile{Name: file.Filename, Reader: opened, ColumnMapping: mapping, Strategy: c.PostForm("strategy")})
	if handleLinuxImportError(c, err) {
		return
	}
	success(c, result)
}

func (h *LinuxHostHandler) ConfirmImport(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	if h.importer == nil {
		failure(c, http.StatusServiceUnavailable, 50321, "linux host importer is unavailable")
		return
	}
	var request struct {
		Token string `json:"token" binding:"required"`
	}
	if c.ShouldBindJSON(&request) != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.importer.Confirm(c.Request.Context(), actor, request.Token)
	if handleLinuxImportError(c, err) {
		return
	}
	success(c, result)
}

func handleLinuxImportError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	recordFailureError(c, err, "linux host import failed")
	switch {
	case errors.Is(err, linuxhostsvc.ErrImportTooManyRows):
		failure(c, http.StatusRequestEntityTooLarge, 41322, err.Error())
	case errors.Is(err, linuxhostsvc.ErrImportFormat), errors.Is(err, linuxhostsvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid import")
	case errors.Is(err, linuxhostsvc.ErrImportExpired):
		failure(c, http.StatusGone, 41001, err.Error())
	case errors.Is(err, linuxhostsvc.ErrImportHasErrors):
		failure(c, http.StatusConflict, 40921, err.Error())
	default:
		failure(c, http.StatusInternalServerError, 50096, "linux host import failed")
	}
	return true
}

func NewLinuxHostHandler(service *linuxhostsvc.Service) *LinuxHostHandler {
	return &LinuxHostHandler{service: service}
}

type saveLinuxHostRequest struct {
	Name                 string          `json:"name" binding:"required"`
	Host                 string          `json:"host" binding:"required"`
	Port                 int             `json:"port"`
	Environment          *string         `json:"environment"`
	SystemName           *string         `json:"systemName"`
	ComponentName        *string         `json:"componentName"`
	Username             *string         `json:"username"`
	AuthType             string          `json:"authType" binding:"required"`
	Password             *string         `json:"password"`
	PrivateKey           *string         `json:"privateKey"`
	PrivateKeyPassphrase *string         `json:"privateKeyPassphrase"`
	CredentialGroupID    *int64          `json:"credentialGroupId"`
	HostKeyPolicy        string          `json:"hostKeyPolicy"`
	HostKeyAlgorithm     *string         `json:"hostKeyAlgorithm"`
	HostKeyFingerprint   *string         `json:"hostKeyFingerprint"`
	ProfileID            *int64          `json:"profileId"`
	Tags                 json.RawMessage `json:"tags"`
	Attributes           json.RawMessage `json:"attributes"`
	Enabled              *bool           `json:"enabled"`
}

type updateLinuxHostRequest struct {
	Name                 *string         `json:"name"`
	Host                 *string         `json:"host"`
	Port                 *int            `json:"port"`
	Environment          *string         `json:"environment"`
	SystemName           *string         `json:"systemName"`
	ComponentName        *string         `json:"componentName"`
	Username             *string         `json:"username"`
	AuthType             *string         `json:"authType"`
	Password             *string         `json:"password"`
	PrivateKey           *string         `json:"privateKey"`
	PrivateKeyPassphrase *string         `json:"privateKeyPassphrase"`
	CredentialGroupID    *int64          `json:"credentialGroupId"`
	HostKeyPolicy        *string         `json:"hostKeyPolicy"`
	HostKeyAlgorithm     *string         `json:"hostKeyAlgorithm"`
	HostKeyFingerprint   *string         `json:"hostKeyFingerprint"`
	ProfileID            *int64          `json:"profileId"`
	Tags                 json.RawMessage `json:"tags"`
	Attributes           json.RawMessage `json:"attributes"`
	Enabled              *bool           `json:"enabled"`
}

type saveCredentialGroupRequest struct {
	Name                 string          `json:"name" binding:"required"`
	AuthType             string          `json:"authType" binding:"required"`
	Username             string          `json:"username" binding:"required"`
	Password             *string         `json:"password"`
	PrivateKey           *string         `json:"privateKey"`
	PrivateKeyPassphrase *string         `json:"privateKeyPassphrase"`
	Scope                json.RawMessage `json:"scope"`
	Enabled              *bool           `json:"enabled"`
}

type updateCredentialGroupRequest struct {
	Name                 *string         `json:"name"`
	AuthType             *string         `json:"authType"`
	Username             *string         `json:"username"`
	Password             *string         `json:"password"`
	PrivateKey           *string         `json:"privateKey"`
	PrivateKeyPassphrase *string         `json:"privateKeyPassphrase"`
	Scope                json.RawMessage `json:"scope"`
	Enabled              *bool           `json:"enabled"`
}

type confirmLinuxHostKeyRequest struct {
	Algorithm   string `json:"algorithm" binding:"required"`
	Fingerprint string `json:"fingerprint" binding:"required"`
}

func (h *LinuxHostHandler) ListHosts(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	hosts, err := h.service.ListHosts(c.Request.Context(), actor)
	if handleLinuxHostError(c, err, "list linux hosts failed") {
		return
	}
	success(c, hosts)
}

func (h *LinuxHostHandler) GetHost(c *gin.Context) {
	actor, id, ok := currentUserAndLinuxID(c)
	if !ok {
		return
	}
	host, err := h.service.GetHost(c.Request.Context(), actor, id)
	if handleLinuxHostError(c, err, "get linux host failed") {
		return
	}
	success(c, host)
}

func (h *LinuxHostHandler) CreateHost(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request saveLinuxHostRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	host, err := h.service.CreateHost(c.Request.Context(), actor, linuxhostsvc.HostInput{
		Name: request.Name, Host: request.Host, Port: request.Port,
		Environment: request.Environment, SystemName: request.SystemName, ComponentName: request.ComponentName,
		Username: request.Username, AuthType: request.AuthType, Password: request.Password,
		PrivateKey: request.PrivateKey, PrivateKeyPassphrase: request.PrivateKeyPassphrase,
		CredentialGroupID: request.CredentialGroupID, HostKeyPolicy: request.HostKeyPolicy, HostKeyAlgorithm: request.HostKeyAlgorithm,
		HostKeyFingerprint: request.HostKeyFingerprint, ProfileID: request.ProfileID,
		Tags: request.Tags, Attributes: request.Attributes, Enabled: enabled,
	})
	if handleLinuxHostError(c, err, "create linux host failed") {
		return
	}
	success(c, host)
}

func (h *LinuxHostHandler) UpdateHost(c *gin.Context) {
	actor, id, ok := currentUserAndLinuxID(c)
	if !ok {
		return
	}
	raw, request, ok := bindLinuxUpdate(c)
	if !ok {
		return
	}
	host, err := h.service.UpdateHost(c.Request.Context(), actor, id, linuxhostsvc.HostUpdateInput{
		Name: request.Name, Host: request.Host, Port: request.Port,
		Environment: request.Environment, EnvironmentSet: hasKey(raw, "environment"),
		SystemName: request.SystemName, SystemNameSet: hasKey(raw, "systemName"),
		ComponentName: request.ComponentName, ComponentNameSet: hasKey(raw, "componentName"),
		Username: request.Username, UsernameSet: hasKey(raw, "username"), AuthType: request.AuthType,
		Password: request.Password, PrivateKey: request.PrivateKey, PrivateKeyPassphrase: request.PrivateKeyPassphrase,
		CredentialGroupID: request.CredentialGroupID, CredentialGroupIDSet: hasKey(raw, "credentialGroupId"),
		HostKeyPolicy: request.HostKeyPolicy, HostKeyAlgorithm: request.HostKeyAlgorithm,
		HostKeyAlgorithmSet: hasKey(raw, "hostKeyAlgorithm"), HostKeyFingerprint: request.HostKeyFingerprint,
		HostKeyFingerprintSet: hasKey(raw, "hostKeyFingerprint"),
		ProfileID:             request.ProfileID, ProfileIDSet: hasKey(raw, "profileId"),
		Tags: request.Tags, TagsSet: hasKey(raw, "tags"), Attributes: request.Attributes,
		AttributesSet: hasKey(raw, "attributes"), Enabled: request.Enabled,
	})
	if handleLinuxHostError(c, err, "update linux host failed") {
		return
	}
	success(c, host)
}

func (h *LinuxHostHandler) DeleteHost(c *gin.Context) {
	actor, id, ok := currentUserAndLinuxID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteHost(c.Request.Context(), actor, id); handleLinuxHostError(c, err, "delete linux host failed") {
		return
	}
	success(c, gin.H{"deleted": true})
}

func (h *LinuxHostHandler) EnableHost(c *gin.Context)  { h.setHostEnabled(c, true) }
func (h *LinuxHostHandler) DisableHost(c *gin.Context) { h.setHostEnabled(c, false) }

func (h *LinuxHostHandler) ConfirmHostKey(c *gin.Context) {
	actor, id, ok := currentUserAndLinuxID(c)
	if !ok {
		return
	}
	var request confirmLinuxHostKeyRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	host, err := h.service.ConfirmHostKey(c.Request.Context(), actor, id, linuxhostsvc.ConfirmHostKeyInput{
		Algorithm: request.Algorithm, Fingerprint: request.Fingerprint,
	})
	if handleLinuxHostError(c, err, "confirm linux host key failed") {
		return
	}
	success(c, host)
}

func (h *LinuxHostHandler) setHostEnabled(c *gin.Context, enabled bool) {
	actor, id, ok := currentUserAndLinuxID(c)
	if !ok {
		return
	}
	host, err := h.service.SetHostEnabled(c.Request.Context(), actor, id, enabled)
	if handleLinuxHostError(c, err, "set linux host enabled state failed") {
		return
	}
	success(c, host)
}

func (h *LinuxHostHandler) ListCredentialGroups(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	groups, err := h.service.ListCredentialGroups(c.Request.Context(), actor)
	if handleLinuxHostError(c, err, "list credential groups failed") {
		return
	}
	success(c, groups)
}

func (h *LinuxHostHandler) GetCredentialGroup(c *gin.Context) {
	actor, id, ok := currentUserAndLinuxID(c)
	if !ok {
		return
	}
	group, err := h.service.GetCredentialGroup(c.Request.Context(), actor, id)
	if handleLinuxHostError(c, err, "get credential group failed") {
		return
	}
	success(c, group)
}

func (h *LinuxHostHandler) CreateCredentialGroup(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request saveCredentialGroupRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	group, err := h.service.CreateCredentialGroup(c.Request.Context(), actor, linuxhostsvc.CredentialGroupInput{
		Name: request.Name, AuthType: request.AuthType, Username: request.Username,
		Password: request.Password, PrivateKey: request.PrivateKey, PrivateKeyPassphrase: request.PrivateKeyPassphrase,
		Scope: request.Scope, Enabled: enabled,
	})
	if handleLinuxHostError(c, err, "create credential group failed") {
		return
	}
	success(c, group)
}

func (h *LinuxHostHandler) UpdateCredentialGroup(c *gin.Context) {
	actor, id, ok := currentUserAndLinuxID(c)
	if !ok {
		return
	}
	var raw map[string]json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	var request updateCredentialGroupRequest
	if !decodeRawRequest(c, raw, &request) {
		return
	}
	group, err := h.service.UpdateCredentialGroup(c.Request.Context(), actor, id, linuxhostsvc.CredentialGroupUpdateInput{
		Name: request.Name, AuthType: request.AuthType, Username: request.Username,
		Password: request.Password, PrivateKey: request.PrivateKey, PrivateKeyPassphrase: request.PrivateKeyPassphrase,
		Scope: request.Scope, ScopeSet: hasKey(raw, "scope"), Enabled: request.Enabled,
	})
	if handleLinuxHostError(c, err, "update credential group failed") {
		return
	}
	success(c, group)
}

func (h *LinuxHostHandler) DeleteCredentialGroup(c *gin.Context) {
	actor, id, ok := currentUserAndLinuxID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteCredentialGroup(c.Request.Context(), actor, id); handleLinuxHostError(c, err, "delete credential group failed") {
		return
	}
	success(c, gin.H{"deleted": true})
}

func bindLinuxUpdate(c *gin.Context) (map[string]json.RawMessage, updateLinuxHostRequest, bool) {
	var raw map[string]json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return nil, updateLinuxHostRequest{}, false
	}
	var request updateLinuxHostRequest
	if !decodeRawRequest(c, raw, &request) {
		return nil, updateLinuxHostRequest{}, false
	}
	return raw, request, true
}

func decodeRawRequest(c *gin.Context, raw map[string]json.RawMessage, target any) bool {
	payload, err := json.Marshal(raw)
	if err == nil {
		err = json.Unmarshal(payload, target)
	}
	if err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return false
	}
	return true
}

func currentUserAndLinuxID(c *gin.Context) (*model.AppUser, int64, bool) {
	actor, ok := currentUser(c)
	if !ok {
		return nil, 0, false
	}
	id, ok := parseLinuxID(c)
	if !ok {
		return nil, 0, false
	}
	return actor, id, true
}

func handleLinuxHostError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	recordFailureError(c, err, fallback)
	switch {
	case errors.Is(err, linuxhostsvc.ErrInvalidInput), errors.Is(err, linuxhostsvc.ErrSensitiveAttribute):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, linuxhostsvc.ErrCredentialConflict), errors.Is(err, repository.ErrConflictingLinuxHostCredentials):
		failure(c, http.StatusConflict, 40901, "credential source conflict")
	case errors.Is(err, repository.ErrLinuxResourceConflict):
		failure(c, http.StatusConflict, 40902, "linux resource already exists")
	case errors.Is(err, repository.ErrCredentialGroupReferenced):
		failure(c, http.StatusConflict, 40903, "credential group is in use")
	case errors.Is(err, linuxhostsvc.ErrCredentialGroupScope):
		failure(c, http.StatusBadRequest, 40021, "credential group is outside host scope")
	case errors.Is(err, linuxhostsvc.ErrHostKeyConfirmationRequired):
		failure(c, http.StatusConflict, 40921, "SSH host key confirmation is required")
	case errors.Is(err, linuxhostsvc.ErrHostKeyMismatch):
		failure(c, http.StatusConflict, 40922, "SSH host key mismatch")
	case errors.Is(err, linuxhostsvc.ErrInsecureHostKeyDisabled):
		failure(c, http.StatusBadRequest, 40022, "insecure SSH host key policy is disabled")
	case errors.Is(err, linuxhostsvc.ErrInvalidHostKey):
		failure(c, http.StatusBadRequest, 40023, "invalid SSH host key")
	case errors.Is(err, linuxhostsvc.ErrAdminRequired), errors.Is(err, linuxhostsvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40307, "linux host access forbidden")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "linux resource not found")
	default:
		failure(c, http.StatusInternalServerError, 50061, fallback)
	}
	return true
}

func parseLinuxID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return 0, false
	}
	return id, true
}
