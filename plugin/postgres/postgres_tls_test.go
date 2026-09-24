package postgres

import (
	"net/url"
	"testing"
)

func TestPreserveCAOnlyTLSMode(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantSNI    string
		wantChange bool
	}{
		{
			name:       "require with root CA",
			source:     "postgresql://user@example:26257/db?sslmode=require&sslrootcert=/certs/ca.crt",
			wantSNI:    "0",
			wantChange: true,
		},
		{
			name:       "default mode with root CA",
			source:     "postgres://user@example:26257/db?sslrootcert=/certs/ca.crt",
			wantSNI:    "0",
			wantChange: true,
		},
		{
			name:       "verify CA",
			source:     "postgresql://user@example:26257/db?sslmode=verify-ca",
			wantSNI:    "0",
			wantChange: true,
		},
		{
			name:    "verify full",
			source:  "postgresql://user@example:26257/db?sslmode=verify-full&sslrootcert=/certs/ca.crt",
			wantSNI: "",
		},
		{
			name:    "require without root CA",
			source:  "postgresql://user@example:26257/db?sslmode=require",
			wantSNI: "",
		},
		{
			name:    "explicit SNI setting",
			source:  "postgresql://user@example:26257/db?sslmode=require&sslrootcert=/certs/ca.crt&sslsni=1",
			wantSNI: "1",
		},
		{
			name:    "keyword DSN",
			source:  "host=example port=26257 sslmode=verify-ca",
			wantSNI: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := preserveCAOnlyTLSMode(tt.source)
			if tt.wantChange == (got == tt.source) {
				t.Fatalf("unexpected change state: got %q", got)
			}

			u, err := url.Parse(got)
			if err != nil {
				t.Fatalf("parse result: %v", err)
			}
			if gotSNI := u.Query().Get("sslsni"); gotSNI != tt.wantSNI {
				t.Fatalf("unexpected sslsni: got %q want %q", gotSNI, tt.wantSNI)
			}
		})
	}
}
