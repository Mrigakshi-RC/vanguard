package service

import (
	"testing"
)

func TestValidateEventStatus(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		wantErr bool
	}{
		{name: "pending", status: "pending", wantErr: false},
		{name: "processing", status: "processing", wantErr: false},
		{name: "completed", status: "completed", wantErr: false},
		{name: "failed", status: "failed", wantErr: true},
		{name: "dead_letter", status: "dead_letter", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEventStatus(tt.status)

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEventStatus() isValid = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
