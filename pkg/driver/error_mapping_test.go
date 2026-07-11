// Copyright 2025 Hewlett Packard Enterprise Development LP
package driver

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// cspHandleErrorString reproduces the error string format produced by the CSP
// storage provider's handleError() helper, which embeds the HTTP status code.
func cspHandleErrorString(httpStatus int) string {
	return fmt.Sprintf("Request failed with status code %d and errors Error code (SOME_CODE) and message (something went wrong)", httpStatus)
}

// connectivityErrorString reproduces the error string format produced by the
// connectivity client's DoJSON() for non-parsable error responses.
func connectivityErrorString(status string) string {
	return fmt.Sprintf("status code was %s for request: action=POST path=/containers/v1/volumes", status)
}

// TestExtractHTTPStatusCode verifies that the HTTP status code is reliably
// extracted from both CSP error string formats and that malformed/missing
// codes are reported as not-found (CROSS-4).
func TestExtractHTTPStatusCode(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
		wantOK   bool
	}{
		{name: "nil error", err: nil, wantOK: false},
		{
			name:     "handleError 409",
			err:      errors.New(cspHandleErrorString(409)),
			wantCode: 409,
			wantOK:   true,
		},
		{
			name:     "handleError 500",
			err:      errors.New(cspHandleErrorString(500)),
			wantCode: 500,
			wantOK:   true,
		},
		{
			name:     "connectivity 503 with status text",
			err:      errors.New(connectivityErrorString("503 Service Unavailable")),
			wantCode: 503,
			wantOK:   true,
		},
		{
			name:     "connectivity 401 with status text",
			err:      errors.New(connectivityErrorString("401 Unauthorized")),
			wantCode: 401,
			wantOK:   true,
		},
		{
			name:   "no status code present",
			err:    errors.New("connection refused"),
			wantOK: false,
		},
		{
			name:   "non-http number ignored (too small)",
			err:    errors.New("status code 7 was odd"),
			wantOK: false,
		},
		{
			name:     "wrapped error still parses",
			err:      fmt.Errorf("Failed to create volume foo, %w", errors.New(cspHandleErrorString(404))),
			wantCode: 404,
			wantOK:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, ok := extractHTTPStatusCode(tc.err)
			assert.Equal(t, tc.wantOK, ok, "unexpected ok for %q", tc.err)
			if tc.wantOK {
				assert.Equal(t, tc.wantCode, code, "unexpected status code for %q", tc.err)
			}
		})
	}
}

// TestGRPCCodeForCSPError verifies the HTTP-to-gRPC mapping, including the
// context-sensitive refinement of HTTP 409 and operation-specific fallbacks
// when no HTTP status is present (CROSS-4).
func TestGRPCCodeForCSPError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		op   cspOperation
		want codes.Code
	}{
		// Direct mappings.
		{name: "400 -> InvalidArgument", err: errors.New(cspHandleErrorString(400)), op: cspOpCreate, want: codes.InvalidArgument},
		{name: "401 -> Unauthenticated", err: errors.New(cspHandleErrorString(401)), op: cspOpCreate, want: codes.Unauthenticated},
		{name: "403 -> PermissionDenied", err: errors.New(cspHandleErrorString(403)), op: cspOpPublish, want: codes.PermissionDenied},
		{name: "404 -> NotFound", err: errors.New(cspHandleErrorString(404)), op: cspOpDelete, want: codes.NotFound},
		{name: "429 -> ResourceExhausted", err: errors.New(cspHandleErrorString(429)), op: cspOpCreate, want: codes.ResourceExhausted},
		{name: "500 -> Internal", err: errors.New(cspHandleErrorString(500)), op: cspOpCreate, want: codes.Internal},
		{name: "501 -> Unimplemented", err: errors.New(cspHandleErrorString(501)), op: cspOpCreate, want: codes.Unimplemented},
		{name: "502 -> Unavailable", err: errors.New(connectivityErrorString("502 Bad Gateway")), op: cspOpCreate, want: codes.Unavailable},
		{name: "503 -> Unavailable", err: errors.New(connectivityErrorString("503 Service Unavailable")), op: cspOpCreate, want: codes.Unavailable},
		{name: "504 -> Unavailable", err: errors.New(connectivityErrorString("504 Gateway Timeout")), op: cspOpExpand, want: codes.Unavailable},

		// HTTP 409 refinement by operation context.
		{name: "409 create -> AlreadyExists", err: errors.New(cspHandleErrorString(409)), op: cspOpCreate, want: codes.AlreadyExists},
		{name: "409 snapshot -> AlreadyExists", err: errors.New(cspHandleErrorString(409)), op: cspOpSnapshot, want: codes.AlreadyExists},
		{name: "409 delete -> FailedPrecondition", err: errors.New(cspHandleErrorString(409)), op: cspOpDelete, want: codes.FailedPrecondition},
		{name: "409 publish -> Aborted", err: errors.New(cspHandleErrorString(409)), op: cspOpPublish, want: codes.Aborted},
		{name: "409 unpublish -> Aborted", err: errors.New(cspHandleErrorString(409)), op: cspOpUnpublish, want: codes.Aborted},

		// Unmapped HTTP code falls back per operation.
		{name: "418 unmapped create -> Internal", err: errors.New(cspHandleErrorString(418)), op: cspOpCreate, want: codes.Internal},
		{name: "418 unmapped unpublish -> Aborted", err: errors.New(cspHandleErrorString(418)), op: cspOpUnpublish, want: codes.Aborted},

		// No HTTP status present -> operation-specific fallback.
		{name: "no status create -> Internal", err: errors.New("connection refused"), op: cspOpCreate, want: codes.Internal},
		{name: "no status unpublish -> Aborted", err: errors.New("connection refused"), op: cspOpUnpublish, want: codes.Aborted},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := gRPCCodeForCSPError(tc.err, tc.op)
			assert.Equal(t, tc.want, got, "unexpected gRPC code for %q (op=%s)", tc.err, tc.op)
		})
	}
}

// TestMapCSPError verifies that MapCSPError returns a gRPC status error with
// both the correct code and the caller-supplied message (CROSS-4).
func TestMapCSPError(t *testing.T) {
	cspErr := errors.New(cspHandleErrorString(401))
	msg := "Failed to create volume foo, " + cspErr.Error()

	mapped := MapCSPError(cspErr, cspOpCreate, msg)

	st, ok := status.FromError(mapped)
	assert.True(t, ok, "MapCSPError must return a gRPC status error")
	assert.Equal(t, codes.Unauthenticated, st.Code(), "401 must map to Unauthenticated")
	assert.Equal(t, msg, st.Message(), "MapCSPError must preserve the caller's message verbatim")
}

// TestMapCSPErrorRetryability documents the key reason for CROSS-4: a CSP 503
// must surface as Unavailable (retryable) rather than Internal (terminal), so
// the external-provisioner/attacher keep retrying transient backend outages.
func TestMapCSPErrorRetryability(t *testing.T) {
	cspErr := errors.New(connectivityErrorString("503 Service Unavailable"))
	mapped := MapCSPError(cspErr, cspOpCreate, cspErr.Error())
	assert.Equal(t, codes.Unavailable, status.Code(mapped),
		"a CSP 503 must be retryable (Unavailable), not terminal (Internal)")
}
