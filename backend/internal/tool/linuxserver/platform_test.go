package linuxserver

import "testing"

func TestDetectPlatformSupportMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		release   string
		kernel    string
		wantState string
	}{
		{name: "rhel9", release: "ID=rhel\nVERSION_ID=9.4", kernel: "Linux 5.14", wantState: CapabilitySupported},
		{name: "centos7", release: "ID=centos\nVERSION_ID=7", kernel: "Linux 3.10", wantState: CapabilitySupported},
		{name: "rocky8", release: "ID=rocky\nVERSION_ID=8.10", kernel: "Linux 4.18", wantState: CapabilitySupported},
		{name: "alma9", release: "ID=almalinux\nVERSION_ID=9.4", kernel: "Linux 5.14", wantState: CapabilitySupported},
		{name: "ubuntu24", release: "ID=ubuntu\nVERSION_ID=24.04", kernel: "Linux 6.8", wantState: CapabilitySupported},
		{name: "debian12", release: "ID=debian\nVERSION_ID=12", kernel: "Linux 6.1", wantState: CapabilitySupported},
		{name: "future ubuntu", release: "ID=ubuntu\nVERSION_ID=26.04", kernel: "Linux 7", wantState: CapabilityPartial},
		{name: "other linux", release: "ID=sles\nVERSION_ID=15", kernel: "Linux 5", wantState: CapabilityPartial},
		{name: "not linux", release: "ID=freebsd\nVERSION_ID=14", kernel: "FreeBSD 14", wantState: CapabilityUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platform := DetectPlatform(tt.release, tt.kernel, []string{"uname"})
			if platform.Status != tt.wantState {
				t.Fatalf("status = %q, want %q", platform.Status, tt.wantState)
			}
		})
	}
}

func TestEvaluateCommandCapabilityUsesCommandsAndOS(t *testing.T) {
	t.Parallel()
	definition, err := NewBuiltinCatalog().Get("diskio.iostat")
	if err != nil {
		t.Fatal(err)
	}
	platform := DetectPlatform("ID=ubuntu\nVERSION_ID=22.04", "Linux", []string{"uname"})
	capability := EvaluateCommandCapability(platform, definition)
	if capability.Status != CapabilityPartial || capability.Runnable || len(capability.MissingCommands) != 1 {
		t.Fatalf("missing command capability = %+v", capability)
	}
	platform = DetectPlatform("ID=sles\nVERSION_ID=15", "Linux", []string{"iostat"})
	capability = EvaluateCommandCapability(platform, definition)
	if capability.Status != CapabilityPartial || !capability.Runnable {
		t.Fatalf("unknown Linux capability = %+v", capability)
	}
	platform = DetectPlatform("ID=freebsd\nVERSION_ID=14", "FreeBSD", []string{"iostat"})
	capability = EvaluateCommandCapability(platform, definition)
	if capability.Status != CapabilityUnsupported || capability.Runnable {
		t.Fatalf("unsupported platform capability = %+v", capability)
	}
}
