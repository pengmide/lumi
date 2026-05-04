package sandbox

import (
	"errors"
	"testing"
)

func TestNormalizeDockerErrorCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "permission denied",
			err:  errors.New("permission denied while connecting to the Docker daemon socket"),
			want: CodeDockerPermissionDenied,
		},
		{
			name: "daemon unavailable",
			err:  errors.New("Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?"),
			want: CodeDockerUnavailable,
		},
		{
			name: "socket missing",
			err:  errors.New("dial unix /var/run/docker.sock: connect: no such file or directory"),
			want: CodeDockerUnavailable,
		},
		{
			name: "unknown",
			err:  errors.New("unexpected docker failure"),
			want: CodeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeDockerError(tt.err)
			if got == nil {
				t.Fatalf("normalizeDockerError() = nil, want %s", tt.want)
			}
			if got.Code != tt.want {
				t.Fatalf("code = %q, want %q", got.Code, tt.want)
			}
			if got.Details == "" {
				t.Fatalf("details is empty")
			}
		})
	}
}

func TestRuntimeErrorResponseUsesCode(t *testing.T) {
	err := errorForCode(CodeImageMissing, "missing test image")
	response := err.Response()

	if response.OK {
		t.Fatalf("OK = true, want false")
	}
	if response.Code != CodeImageMissing {
		t.Fatalf("Code = %q, want %q", response.Code, CodeImageMissing)
	}
	if response.Message == "" {
		t.Fatalf("Message is empty")
	}
	if response.Details == "" {
		t.Fatalf("Details is empty")
	}
}
