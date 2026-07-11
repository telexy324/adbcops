package sshsftp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestSecurePathRejectsTraversalSensitiveAndSymlinkEscape(t *testing.T) {
	client := newFakeClient()
	client.files["/var/log/app/app.log"] = fakeFile{content: "ok"}
	client.symlinks["/var/log/app/etc-link"] = "/etc/passwd"

	if _, err := SecurePath(client, "/var/log/app/../secret.log", []string{"/var/log/app"}); err != ErrPathTraversal {
		t.Fatalf("traversal error = %v, want ErrPathTraversal", err)
	}
	if _, err := SecurePath(client, "/etc/passwd", []string{"/var/log/app"}); err != ErrSensitivePath {
		t.Fatalf("sensitive error = %v, want ErrSensitivePath", err)
	}
	if _, err := SecurePath(client, "/var/log/app/etc-link", []string{"/var/log/app"}); err != ErrPathNotAllowed {
		t.Fatalf("symlink escape error = %v, want ErrPathNotAllowed", err)
	}
}

func TestReadFileUsesDecryptedCredentialAndEnforcesAllowlist(t *testing.T) {
	client := newFakeClient()
	client.files["/var/log/app/app.log"] = fakeFile{content: "hello from sftp"}
	dialer := &fakeDialer{client: client}
	service := NewService(newFakeRepository(t), &fakeSecrets{}, dialer)

	result, err := service.ReadFile(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, ReadInput{
		DataSourceID: 1,
		Path:         "/var/log/app/app.log",
		MaxBytes:     1024,
	})
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if result.Content != "hello from sftp" || result.Path != "/var/log/app/app.log" {
		t.Fatalf("result = %+v", result)
	}
	if dialer.last.Username != "ops" || dialer.last.Password != "secret-password" {
		t.Fatalf("connection config = %+v", dialer.last)
	}

	_, err = service.ReadFile(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, ReadInput{
		DataSourceID: 1,
		Path:         "/var/log/other.log",
	})
	if err != ErrPathNotAllowed {
		t.Fatalf("outside allowlist error = %v, want ErrPathNotAllowed", err)
	}
}

func TestReadFileRejectsOversizedFile(t *testing.T) {
	client := newFakeClient()
	client.files["/var/log/app/big.log"] = fakeFile{content: strings.Repeat("x", 10)}
	service := NewService(newFakeRepository(t), &fakeSecrets{}, &fakeDialer{client: client})
	_, err := service.ReadFile(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, ReadInput{
		DataSourceID: 1,
		Path:         "/var/log/app/big.log",
		MaxBytes:     4,
	})
	if err != ErrFileTooLarge {
		t.Fatalf("ReadFile(big) error = %v, want ErrFileTooLarge", err)
	}
}

type fakeRepository struct {
	source *model.DataSource
}

func newFakeRepository(t *testing.T) *fakeRepository {
	t.Helper()
	config, _ := json.Marshal(Config{
		Host:      "sftp.example",
		Port:      22,
		Allowlist: []string{"/var/log/app"},
		MaxBytes:  1024,
	})
	credentialID := int64(1)
	return &fakeRepository{source: &model.DataSource{
		ID:           1,
		Name:         "prod-sftp",
		SourceType:   model.DataSourceTypeSSH,
		Config:       config,
		CredentialID: &credentialID,
		Credential: &model.CredentialSecret{
			ID:               credentialID,
			EncryptedPayload: "encrypted:" + base64.RawURLEncoding.EncodeToString([]byte(`{"username":"ops","password":"secret-password"}`)),
		},
		Enabled:  true,
		ReadOnly: true,
	}}
}

func (f *fakeRepository) FindDataSourceByID(_ context.Context, id int64) (*model.DataSource, error) {
	if id != f.source.ID {
		return nil, repository.ErrNotFound
	}
	return f.source, nil
}

type fakeSecrets struct{}

func (f *fakeSecrets) Decrypt(value string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "encrypted:"))
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

type fakeDialer struct {
	client *fakeClient
	last   ConnectionConfig
}

func (f *fakeDialer) Open(_ context.Context, config ConnectionConfig) (SFTPClient, error) {
	f.last = config
	return f.client, nil
}

type fakeClient struct {
	files    map[string]fakeFile
	symlinks map[string]string
}

func newFakeClient() *fakeClient {
	return &fakeClient{files: make(map[string]fakeFile), symlinks: make(map[string]string)}
}

func (f *fakeClient) Open(path string) (io.ReadCloser, error) {
	file, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(strings.NewReader(file.content)), nil
}

func (f *fakeClient) Lstat(path string) (FileInfo, error) {
	if _, ok := f.symlinks[path]; ok {
		return fakeInfo{mode: os.ModeSymlink}, nil
	}
	if file, ok := f.files[path]; ok {
		return fakeInfo{size: int64(len(file.content))}, nil
	}
	for filePath := range f.files {
		if strings.HasPrefix(filePath, strings.TrimRight(path, "/")+"/") {
			return fakeInfo{dir: true}, nil
		}
	}
	for linkPath := range f.symlinks {
		if strings.HasPrefix(linkPath, strings.TrimRight(path, "/")+"/") {
			return fakeInfo{dir: true}, nil
		}
	}
	return nil, os.ErrNotExist
}

func (f *fakeClient) ReadLink(path string) (string, error) {
	target, ok := f.symlinks[path]
	if !ok {
		return "", os.ErrNotExist
	}
	return target, nil
}

func (f *fakeClient) Close() error {
	return nil
}

type fakeFile struct {
	content string
}

type fakeInfo struct {
	size int64
	mode os.FileMode
	dir  bool
}

func (f fakeInfo) IsDir() bool {
	return f.dir
}

func (f fakeInfo) Size() int64 {
	return f.size
}

func (f fakeInfo) Mode() os.FileMode {
	return f.mode
}
