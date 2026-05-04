package sandbox

import (
	"errors"
	"fmt"
	"strings"
)

type RuntimeError struct {
	Code        string
	Message     string
	Details     string
	Recoverable bool
}

func (e *RuntimeError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if strings.TrimSpace(e.Code) != "" {
		return e.Code
	}
	return CodeUnknown
}

func (e *RuntimeError) Response() PreflightResponse {
	if e == nil {
		return PreflightResponse{
			OK:          true,
			Code:        CodeReady,
			Recoverable: true,
		}
	}
	return PreflightResponse{
		OK:          false,
		Code:        e.Code,
		Message:     e.Message,
		Recoverable: e.Recoverable,
		Details:     e.Details,
	}
}

func errorForCode(code string, details string) *RuntimeError {
	message := map[string]string{
		CodePathInvalid:                 "Invalid workspace path",
		CodeDockerUnavailable:           "Cannot connect to Docker",
		CodeDockerPermissionDenied:      "Docker permission denied",
		CodeImageMissing:                "Sandbox image is missing locally",
		CodeImagePullFailed:             "Failed to pull sandbox image",
		CodeHostConnectUnresolved:       "Cannot resolve host callback address",
		CodeExecutorRegistrationTimeout: "Sandbox executor did not connect in time",
		CodeSandboxUnavailable:          "Sandbox runtime is unavailable",
		CodeUnknown:                     "Sandbox runtime failed",
	}[code]
	if message == "" {
		message = "Sandbox runtime failed"
	}
	return &RuntimeError{
		Code:        code,
		Message:     message,
		Details:     strings.TrimSpace(details),
		Recoverable: code != CodePathInvalid,
	}
}

func normalizeDockerError(err error) *RuntimeError {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "permission denied"):
		return errorForCode(CodeDockerPermissionDenied, err.Error())
	case strings.Contains(msg, "cannot connect to the docker daemon"),
		strings.Contains(msg, "is the docker daemon running"),
		strings.Contains(msg, "no such file or directory"),
		strings.Contains(msg, "connect: connection refused"):
		return errorForCode(CodeDockerUnavailable, err.Error())
	default:
		return errorForCode(CodeUnknown, err.Error())
	}
}

func wrapRuntimeError(code string, err error) *RuntimeError {
	if err == nil {
		return nil
	}
	var runtimeErr *RuntimeError
	if errors.As(err, &runtimeErr) {
		return runtimeErr
	}
	if code == "" {
		return normalizeDockerError(err)
	}
	return errorForCode(code, err.Error())
}

func invalidPathError(path string, err error) *RuntimeError {
	details := strings.TrimSpace(path)
	if err != nil {
		if details != "" {
			details = fmt.Sprintf("%s: %v", details, err)
		} else {
			details = err.Error()
		}
	}
	return errorForCode(CodePathInvalid, details)
}
