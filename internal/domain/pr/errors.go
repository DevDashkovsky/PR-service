package pr

import "errors"

const (
	PRExists      = "PR_EXISTS"
	PRMerged      = "PR_MERGED"
	NotAssigned   = "NOT_ASSIGNED"
	NoCandidate   = "NO_CANDIDATE"
	NotFound      = "NOT_FOUND"
	BadRequest    = "BAD_REQUEST"
	InternalError = "INTERNAL_ERROR"
)

var (
	ErrPRExists            = errors.New("pull request already exists")
	ErrPRMerged            = errors.New("cannot modify merged pull request")
	ErrNotAssigned         = errors.New("reviewer is not assigned to this PR")
	ErrNoCandidate         = errors.New("no active replacement candidate in team")
	ErrPRNotFound          = errors.New("pull request not found")
	ErrAuthorNotFound      = errors.New("author not found")
	ErrAuthorNoTeam        = errors.New("author has no team")
	ErrTransactionBusy     = errors.New("transaction retries exhausted")
	ErrIdempotencyConflict = errors.New("idempotency key was used for a different request")
)
