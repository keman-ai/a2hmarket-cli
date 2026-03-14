package api

import (
	"strings"
	"testing"
)

func TestPlatformError_Error_WithHTTPStatus(t *testing.T) {
	err := &PlatformError{
		PlatformCode: "401",
		Message:      "unauthorized",
		HTTPStatus:   401,
	}

	got := err.Error()
	want := "platform error: HTTP 401, code=401, message=unauthorized"

	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestPlatformError_Error_WithoutHTTPStatus(t *testing.T) {
	err := &PlatformError{
		PlatformCode: "500",
		Message:      "internal error",
		HTTPStatus:   0,
	}

	got := err.Error()
	want := "platform error: code=500, message=internal error"

	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestPlatformError_Error_Variants(t *testing.T) {
	tests := []struct {
		name       string
		err        *PlatformError
		wantPrefix string
		wantParts  []string
	}{
		{
			name: "HTTP 500 server error",
			err: &PlatformError{
				PlatformCode: "500",
				Message:      "server error",
				HTTPStatus:   500,
			},
			wantPrefix: "platform error:",
			wantParts:  []string{"HTTP 500", "code=500", "message=server error"},
		},
		{
			name: "HTTP 403 forbidden",
			err: &PlatformError{
				PlatformCode: "403",
				Message:      "forbidden",
				HTTPStatus:   403,
			},
			wantPrefix: "platform error:",
			wantParts:  []string{"HTTP 403", "code=403", "message=forbidden"},
		},
		{
			name: "business error without HTTP status",
			err: &PlatformError{
				PlatformCode: "10001",
				Message:      "invalid parameter",
				HTTPStatus:   0,
			},
			wantPrefix: "platform error:",
			wantParts:  []string{"code=10001", "message=invalid parameter"},
		},
		{
			name: "empty message",
			err: &PlatformError{
				PlatformCode: "999",
				Message:      "",
				HTTPStatus:   0,
			},
			wantPrefix: "platform error:",
			wantParts:  []string{"code=999", "message="},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()

			if !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("Error() = %q, want prefix %q", got, tt.wantPrefix)
			}

			for _, part := range tt.wantParts {
				if !strings.Contains(got, part) {
					t.Errorf("Error() = %q, want to contain %q", got, part)
				}
			}
		})
	}
}

func TestPlatformError_ImplementsErrorInterface(t *testing.T) {
	var err error = &PlatformError{
		PlatformCode: "200",
		Message:      "ok",
	}

	if err == nil {
		t.Error("PlatformError should implement error interface")
	}
}

func TestNewPlatformError(t *testing.T) {
	err := newPlatformError("404", "not found", 404)

	if err.PlatformCode != "404" {
		t.Errorf("PlatformCode = %q, want %q", err.PlatformCode, "404")
	}
	if err.Message != "not found" {
		t.Errorf("Message = %q, want %q", err.Message, "not found")
	}
	if err.HTTPStatus != 404 {
		t.Errorf("HTTPStatus = %d, want %d", err.HTTPStatus, 404)
	}
}
