package migrations

import "testing"

func TestRollbackAllowed(t *testing.T) {
	tests := []struct {
		environment string
		want        bool
	}{
		{environment: "dev", want: true},
		{environment: "test", want: true},
		{environment: "prod", want: false},
		{environment: "production", want: false},
		{environment: " PRODUCTION ", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.environment, func(t *testing.T) {
			if got := RollbackAllowed(tt.environment); got != tt.want {
				t.Fatalf("RollbackAllowed(%q) = %v, want %v", tt.environment, got, tt.want)
			}
		})
	}
}
