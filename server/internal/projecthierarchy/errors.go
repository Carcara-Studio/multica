package projecthierarchy

import (
	"errors"
	"fmt"
)

// ErrorCode is a stable internal domain code that future API/CLI adapters may
// map to transport-specific responses without parsing error strings.
type ErrorCode string

const (
	CodeInvalidInput           ErrorCode = "invalid_input"
	CodeProjectNotFound        ErrorCode = "project_not_found"
	CodeMembershipNotFound     ErrorCode = "membership_not_found"
	CodeMembershipExists       ErrorCode = "membership_exists"
	CodeAgentNotEligible       ErrorCode = "agent_not_eligible"
	CodeAgentArchived          ErrorCode = "agent_archived"
	CodeManagerNotActive       ErrorCode = "manager_not_active"
	CodeRootRequired           ErrorCode = "root_required"
	CodeRootChangeRequiresBulk ErrorCode = "root_change_requires_bulk"
	CodeInvalidRootCount       ErrorCode = "invalid_root_count"
	CodeCycleDetected          ErrorCode = "cycle_detected"
	CodeOrphaningMembership    ErrorCode = "orphaning_membership"
	CodeDuplicateChange        ErrorCode = "duplicate_change"
)

// DomainError represents an expected hierarchy-domain rejection. Infrastructure
// failures are returned as ordinary wrapped errors and must not be mistaken for
// a caller conflict.
type DomainError struct {
	Code   ErrorCode
	Detail string
}

func (e *DomainError) Error() string {
	if e.Detail == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Detail)
}

func domainError(code ErrorCode, format string, args ...any) error {
	return &DomainError{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// IsCode reports whether err contains a hierarchy DomainError with code.
func IsCode(err error, code ErrorCode) bool {
	var domainErr *DomainError
	return errors.As(err, &domainErr) && domainErr.Code == code
}
