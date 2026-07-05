package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAllowOrigins(t *testing.T) {
	tests := []struct {
		name    string
		origins string
		want    []string
	}{
		{
			name:    "empty",
			origins: "",
		},
		{
			name:    "trims whitespace and trailing slash",
			origins: " http://localhost:5173/ , *.sciedu.sdc.nycu.club ",
			want:    []string{"http://localhost:5173", "*.sciedu.sdc.nycu.club"},
		},
		{
			name:    "ignores empty parts",
			origins: ", http://localhost:5173/,,",
			want:    []string{"http://localhost:5173"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, parseAllowOrigins(tt.origins))
		})
	}
}
