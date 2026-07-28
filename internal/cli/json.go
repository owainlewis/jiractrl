package cli

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/owainlewis/jiractrl/internal/jira"
)

type successEnvelope struct {
	OK   bool `json:"ok"`
	Data any  `json:"data"`
}

type errorEnvelope struct {
	OK    bool          `json:"ok"`
	Error contractError `json:"error"`
}

type contractError struct {
	Kind          string            `json:"kind"`
	Message       string            `json:"message"`
	Status        int               `json:"status,omitempty"`
	ErrorMessages []string          `json:"errorMessages,omitempty"`
	Fields        map[string]string `json:"fields,omitempty"`
	Retry         *retryMetadata    `json:"retry,omitempty"`
	Candidates    any               `json:"candidates,omitempty"`
}

type retryMetadata struct {
	Attempts          int    `json:"attempts"`
	Retryable         bool   `json:"retryable"`
	RetryAfterSeconds int64  `json:"retryAfterSeconds,omitempty"`
	Reason            string `json:"reason,omitempty"`
	Limit             string `json:"limit,omitempty"`
	Remaining         string `json:"remaining,omitempty"`
	Reset             string `json:"reset,omitempty"`
	NearLimit         string `json:"nearLimit,omitempty"`
}

type reportedError struct {
	err error
}

func (e *reportedError) Error() string { return e.err.Error() }
func (e *reportedError) Unwrap() error { return e.err }

func IsReported(err error) bool {
	var reported *reportedError
	return errors.As(err, &reported)
}

func ExitCode(err error) int {
	var jiraErr *jira.Error
	if errors.As(err, &jiraErr) {
		switch jiraErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return 2
		case http.StatusNotFound:
			return 3
		case http.StatusTooManyRequests:
			return 6
		case http.StatusConflict:
			return 7
		default:
			if jiraErr.StatusCode >= 500 {
				return 5
			}
			if jiraErr.StatusCode >= 400 {
				return 4
			}
		}
	}
	return 1
}

func writeSuccessJSON(w io.Writer, value any) error {
	return writeJSON(w, successEnvelope{OK: true, Data: value})
}

func writeErrorJSON(w io.Writer, err error) error {
	return writeJSON(w, errorEnvelope{OK: false, Error: classifyError(err)})
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func classifyError(err error) contractError {
	result := contractError{
		Kind:    "local",
		Message: err.Error(),
	}

	var unsupported *jira.UnsupportedCapabilityError
	if errors.As(err, &unsupported) {
		result.Kind = "unsupported"
		return result
	}
	var validation *jira.ValidationError
	if errors.As(err, &validation) {
		result.Kind = "validation"
		result.Fields = map[string]string{validation.Field: validation.Message}
		return result
	}
	var ambiguous *jira.AmbiguousMatchError
	if errors.As(err, &ambiguous) {
		result.Kind = "ambiguous"
		result.Candidates = ambiguous.Candidates
		return result
	}
	var jiraErr *jira.Error
	if !errors.As(err, &jiraErr) {
		var networkErr net.Error
		if errors.As(err, &networkErr) {
			result.Kind = "transport"
		}
		return result
	}

	result.Status = jiraErr.StatusCode
	result.ErrorMessages = jiraErr.ErrorMessages
	result.Fields = jiraErr.FieldErrors
	switch jiraErr.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		result.Kind = "auth"
	case http.StatusNotFound:
		result.Kind = "not_found"
	case http.StatusTooManyRequests:
		result.Kind = "rate_limited"
	case http.StatusConflict:
		result.Kind = "conflict"
	default:
		if jiraErr.StatusCode >= 500 {
			result.Kind = "server"
		} else {
			result.Kind = "validation"
		}
	}
	attempts := jiraErr.Attempts
	if attempts < 1 {
		attempts = 1
	}
	result.Retry = &retryMetadata{
		Attempts:          attempts,
		Retryable:         jiraErr.StatusCode == http.StatusTooManyRequests || isRetryableServerStatus(jiraErr.StatusCode),
		RetryAfterSeconds: int64((jiraErr.RetryAfter + time.Second - 1) / time.Second),
		Reason:            jiraErr.RateLimitReason,
		Limit:             jiraErr.RateLimitLimit,
		Remaining:         jiraErr.RateLimitRemain,
		Reset:             jiraErr.RateLimitReset,
		NearLimit:         jiraErr.RateLimitNear,
	}
	return result
}

func isRetryableServerStatus(status int) bool {
	switch status {
	case http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func wantsJSON(args []string) bool {
	dryRun := false
	for _, arg := range args {
		if arg == "--json" || arg == "--raw-json" ||
			arg == "--json=true" || arg == "--raw-json=true" {
			return true
		}
		if arg == "--dry-run" || arg == "--dry-run=true" {
			dryRun = true
		}
	}
	if !dryRun {
		return false
	}
	command := commandName(args)
	return command == "create" || command == "update" || command == "transition"
}

func commandName(args []string) string {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--config":
			i++
		case strings.HasPrefix(args[i], "--config="):
		case !strings.HasPrefix(args[i], "-"):
			return args[i]
		}
	}
	return ""
}
