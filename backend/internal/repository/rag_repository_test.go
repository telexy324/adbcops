package repository

import (
	"reflect"
	"testing"
)

func TestSystemNameVariants(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "query includes suffix", value: "全国教育3系统", want: []string{"全国教育3系统", "全国教育3"}},
		{name: "stored name may include suffix", value: "全国教育3", want: []string{"全国教育3", "全国教育3系统"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := systemNameVariants(tt.value); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("systemNameVariants(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
