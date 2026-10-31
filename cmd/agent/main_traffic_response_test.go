package main

import "testing"

func TestValidateTrafficResponse(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "complete success", body: `{"success":true,"obj":{"accepted":4,"dropped":0,"rejected":0}}`},
		{name: "dropped samples", body: `{"success":true,"obj":{"accepted":3,"dropped":1,"rejected":0}}`, wantErr: true},
		{name: "rejected samples", body: `{"success":true,"obj":{"accepted":3,"dropped":0,"rejected":1}}`, wantErr: true},
		{name: "api failure", body: `{"success":false,"msg":"unavailable"}`, wantErr: true},
		{name: "invalid response", body: `not-json`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTrafficResponse([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateTrafficResponse() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
