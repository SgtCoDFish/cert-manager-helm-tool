package parser

import (
	"reflect"
	"testing"
)

func TestParsePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected Path
		wantErr  bool
	}{
		{
			name:     "Empty path",
			path:     "",
			expected: Path{},
			wantErr:  false,
		},
		{
			name:     "Single string component",
			path:     "foo",
			expected: Path{stringPathComponent("foo")},
			wantErr:  false,
		},
		{
			name:     "Single index component",
			path:     "foo[0]",
			expected: Path{stringPathComponent("foo"), indexPathComponent(0)},
			wantErr:  false,
		},
		{
			name:     "Multiple components",
			path:     "foo.bar[0].baz",
			expected: Path{stringPathComponent("foo"), stringPathComponent("bar"), indexPathComponent(0), stringPathComponent("baz")},
			wantErr:  false,
		},
		{
			name:     "Invalid path 2",
			path:     "foo[0]aa",
			expected: Path{stringPathComponent("foo"), indexPathComponent(0)},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParsePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("ParsePath() = %v, expected %v", got, tt.expected)
			}
		})
	}
}
