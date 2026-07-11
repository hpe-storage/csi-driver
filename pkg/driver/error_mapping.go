// Copyright 2025 Hewlett Packard Enterprise Development LP

package driver

import (
	"regexp"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	log "github.com/hpe-storage/common-host-libs/logger"
)

// CROSS-4: CSP Error Code Propagation.
//
// The CSI driver talks to the Container Storage Provider (CSP) over HTTP. When
// the CSP fails, it embeds the HTTP status code in the returned error string,
// e.g.:
//
//	"Request failed with status code 409 and errors Error code (...) and message (...)"
//	"status code was 503 Service Unavailable for request: action=POST path=/..."
//
// Historically the driver wrapped every one of these errors as
// codes.Internal, which is wrong per the CSI spec and confuses the Kubernetes
// external-provisioner / external-attacher retry logic (Internal is treated as
// a terminal error, while Unavailable/Aborted are retried). This file provides
// a single, explicit HTTP-to-gRPC mapping so CSP errors surface with the
// correct gRPC status code.

// httpToGRPCCode maps CSP HTTP status codes to the correct gRPC status codes
// per the CSI spec requirements. Ambiguous codes (e.g. 409) are refined by the
// calling operation via gRPCCodeForCSPError.
var httpToGRPCCode = map[int]codes.Code{
	400: codes.InvalidArgument,
	401: codes.Unauthenticated,
	403: codes.PermissionDenied,
	404: codes.NotFound,
	409: codes.AlreadyExists, // For "already exists" conflicts (refined per-op)
	422: codes.InvalidArgument,
	429: codes.ResourceExhausted,
	500: codes.Internal,
	501: codes.Unimplemented,
	502: codes.Unavailable,
	503: codes.Unavailable,
	504: codes.Unavailable,
}

// cspOperation identifies the CSI operation that triggered a CSP call. It is
// used to refine ambiguous HTTP status mappings (notably HTTP 409) and to
// choose an appropriate fallback code when the HTTP status cannot be
// determined from the error string.
type cspOperation string

const (
	cspOpCreate    cspOperation = "create"
	cspOpDelete    cspOperation = "delete"
	cspOpPublish   cspOperation = "publish"
	cspOpUnpublish cspOperation = "unpublish"
	cspOpSnapshot  cspOperation = "snapshot"
	cspOpExpand    cspOperation = "expand"
	cspOpGet       cspOperation = "get"
)

// cspStatusCodeRegexp extracts the HTTP status code embedded in a CSP error
// string. It matches both error formats produced downstream:
//
//	handleError():      "... status code 409 and errors ..."
//	connectivity.DoJSON: "status code was 503 Service Unavailable ..."
//
// The optional "was" and flexible whitespace cover both spellings, and the
// three-digit capture is range-validated by the caller.
var cspStatusCodeRegexp = regexp.MustCompile(`(?i)status code(?:\s+was)?\s+(\d{3})`)

// extractHTTPStatusCode parses the HTTP status code embedded in a CSP error
// string. It returns the status code and true when a plausible HTTP status
// (100-599) is found, otherwise 0 and false.
func extractHTTPStatusCode(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	matches := cspStatusCodeRegexp.FindStringSubmatch(err.Error())
	if len(matches) < 2 {
		return 0, false
	}
	code, convErr := strconv.Atoi(matches[1])
	if convErr != nil || code < 100 || code > 599 {
		return 0, false
	}
	return code, true
}

// fallbackCode returns the gRPC code to use when the HTTP status cannot be
// extracted from a CSP error (e.g. a connection-level failure that never
// reached the array). Most operations fall back to Internal, preserving the
// historical behavior. Unpublish falls back to Aborted so the external-attacher
// keeps retrying transient detach failures rather than treating them as
// terminal.
func fallbackCode(op cspOperation) codes.Code {
	if op == cspOpUnpublish {
		return codes.Aborted
	}
	return codes.Internal
}

// refineConflictCode maps an HTTP 409 (Conflict) to the most appropriate gRPC
// code for the given operation:
//
//   - delete            -> FailedPrecondition (resource still in use)
//   - publish/unpublish -> Aborted            (conflicts with an in-flight op)
//   - create/snapshot   -> AlreadyExists      (resource already exists)
//
// This implements the CSI-spec guidance that a 409 is either AlreadyExists or
// Aborted/FailedPrecondition depending on context.
func refineConflictCode(op cspOperation) codes.Code {
	switch op {
	case cspOpDelete:
		return codes.FailedPrecondition
	case cspOpPublish, cspOpUnpublish:
		return codes.Aborted
	default: // create, snapshot, expand, get
		return codes.AlreadyExists
	}
}

// gRPCCodeForCSPError determines the gRPC status code for a CSP error by
// extracting its embedded HTTP status code and applying the explicit mapping,
// refining ambiguous codes using the operation context. When no HTTP status
// can be determined, an operation-specific fallback is used.
func gRPCCodeForCSPError(cspErr error, op cspOperation) codes.Code {
	httpStatus, ok := extractHTTPStatusCode(cspErr)
	if !ok {
		return fallbackCode(op)
	}

	if httpStatus == 409 {
		return refineConflictCode(op)
	}

	code, mapped := httpToGRPCCode[httpStatus]
	if !mapped {
		return fallbackCode(op)
	}
	return code
}

// MapCSPError converts an error returned by the CSP storage provider into a
// gRPC status error whose code reflects the CSP's HTTP status code (CROSS-4).
//
// `message` is the fully-formatted, user-facing error message and is used
// verbatim, so call sites keep the descriptive text they already build (which
// commonly embeds cspErr.Error()); only the gRPC code is corrected. `op`
// refines ambiguous mappings such as HTTP 409 and selects the fallback code
// when the HTTP status is not present in the error.
func MapCSPError(cspErr error, op cspOperation, message string) error {
	code := gRPCCodeForCSPError(cspErr, op)
	log.Tracef("Mapping CSP error to gRPC code %s for %s operation: %v", code, op, cspErr)
	return status.Error(code, message)
}
