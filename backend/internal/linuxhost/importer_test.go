package linuxhost

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
	"github.com/xuri/excelize/v2"
)

func TestBatchImportPreviewDoesNotWriteAndConfirmCreates(t *testing.T) {
	service := &fakeImportHostService{}
	importer, err := NewBatchImporter(service, t.TempDir(), time.Hour, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := importer.Preview(context.Background(), importAdmin(), ImportFile{Name: "hosts.csv", Reader: strings.NewReader("name,host,environment,username,auth_type,password,tags\napp01,10.0.0.1,prod,ops,password,super-secret,app|prod\n")})
	if err != nil {
		t.Fatal(err)
	}
	if service.creates != 0 || preview.Valid != 1 || preview.Strategy != DuplicateSkip {
		t.Fatalf("preview=%+v creates=%d", preview, service.creates)
	}
	if strings.Contains(string(mustMarshal(preview)), "super-secret") {
		t.Fatal("preview leaked password")
	}
	result, err := importer.Confirm(context.Background(), importAdmin(), preview.Token)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 1 || service.creates != 1 || result.TransactionPolicy != "per_row_atomic_continue_on_error" {
		t.Fatalf("result=%+v", result)
	}
}

func TestBatchImportCSVTSVAndXLSXColumnMapping(t *testing.T) {
	for _, test := range []struct {
		name    string
		data    []byte
		mapping map[string]string
	}{
		{"mapped.csv", []byte("Server,Address,Login,Secret\napp,10.0.0.2,ops,pw\n"), map[string]string{"name": "Server", "host": "Address", "username": "Login", "password": "Secret"}},
		{"hosts.tsv", []byte("name\thost\tusername\tpassword\napp\t10.0.0.3\tops\tpw\n"), nil},
		{"hosts.xlsx", xlsxImportBytes(t), nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeImportHostService{}
			importer, _ := NewBatchImporter(service, t.TempDir(), time.Hour, 2<<20)
			preview, err := importer.Preview(context.Background(), importAdmin(), ImportFile{Name: test.name, Reader: bytes.NewReader(test.data), ColumnMapping: test.mapping})
			if err != nil || preview.Valid != 1 {
				t.Fatalf("preview=%+v err=%v", preview, err)
			}
		})
	}
}

func TestBatchImportDuplicateDefaultsToSkipAndDoesNotReplaceCredential(t *testing.T) {
	service := &fakeImportHostService{hosts: []HostView{{ID: 9, Name: "old", Host: "10.0.0.1", Port: 22, Environment: stringPointer("prod"), AuthType: model.LinuxAuthTypePassword}}}
	importer, _ := NewBatchImporter(service, t.TempDir(), time.Hour, 1<<20)
	preview, err := importer.Preview(context.Background(), importAdmin(), ImportFile{Name: "hosts.csv", Reader: strings.NewReader("name,host,environment,username,password\nnew,10.0.0.1,prod,ops,new-secret\n")})
	if err != nil || preview.Duplicates != 1 || preview.Rows[0].Action != DuplicateSkip {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	result, err := importer.Confirm(context.Background(), importAdmin(), preview.Token)
	if err != nil || result.Skipped != 1 || service.updates != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestBatchImportErrorsNeverContainCredentialAndBlockConfirm(t *testing.T) {
	importer, _ := NewBatchImporter(&fakeImportHostService{}, t.TempDir(), time.Hour, 1<<20)
	preview, err := importer.Preview(context.Background(), importAdmin(), ImportFile{Name: "hosts.csv", Reader: strings.NewReader("name,host,username,password\n,10.0.0.1,ops,highly-secret\n")})
	if err != nil || preview.Invalid != 1 {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	if strings.Contains(string(mustMarshal(preview)), "highly-secret") {
		t.Fatal("error report leaked credential")
	}
	if _, err := importer.Confirm(context.Background(), importAdmin(), preview.Token); err != ErrImportHasErrors {
		t.Fatalf("confirm error=%v", err)
	}
}

func TestBatchImportMaxRowsAndExpiredTempCleanup(t *testing.T) {
	var data strings.Builder
	writer := csv.NewWriter(&data)
	_ = writer.Write([]string{"name", "host", "username", "password"})
	for i := 0; i <= MaxLinuxImportRows; i++ {
		_ = writer.Write([]string{"app", "10.0.0.1", "ops", "pw"})
	}
	writer.Flush()
	dir := t.TempDir()
	importer, _ := NewBatchImporter(&fakeImportHostService{}, dir, time.Millisecond, 4<<20)
	if _, err := importer.Preview(context.Background(), importAdmin(), ImportFile{Name: "hosts.csv", Reader: strings.NewReader(data.String())}); err != ErrImportTooManyRows {
		t.Fatalf("error=%v", err)
	}
	path := filepath.Join(dir, "linux-import-orphan")
	_ = os.WriteFile(path, []byte("secret"), 0600)
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(path, old, old)
	if err := importer.CleanupExpired(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("orphan still exists: %v", err)
	}
}

type fakeImportHostService struct {
	hosts            []HostView
	creates, updates int
}

func (f *fakeImportHostService) ListHosts(context.Context, *model.AppUser) ([]HostView, error) {
	return f.hosts, nil
}
func (f *fakeImportHostService) ListCredentialGroups(context.Context, *model.AppUser) ([]CredentialGroupView, error) {
	return nil, nil
}
func (f *fakeImportHostService) CreateHost(_ context.Context, _ *model.AppUser, input HostInput) (*HostView, error) {
	f.creates++
	return &HostView{ID: int64(100 + f.creates), Name: input.Name}, nil
}
func (f *fakeImportHostService) UpdateHost(_ context.Context, _ *model.AppUser, id int64, _ HostUpdateInput) (*HostView, error) {
	f.updates++
	return &HostView{ID: id}, nil
}
func importAdmin() *model.AppUser    { return &model.AppUser{ID: 1, Role: model.RoleAdmin} }
func stringPointer(v string) *string { return &v }
func mustMarshal(v any) []byte       { b, _ := json.Marshal(v); return b }
func xlsxImportBytes(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	rows := [][]string{{"name", "host", "username", "password"}, {"app", "10.0.0.4", "ops", "pw"}}
	for r, row := range rows {
		for c, v := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
			_ = f.SetCellValue("Sheet1", cell, v)
		}
	}
	buffer, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
