package formations

import (
	"fmt"
	"regexp"
)

var safeBeadsIssueIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*-[a-z0-9]+(\.[0-9]+)*$`)

func isSafeBeadsIssueID(value string) bool {
	return safeBeadsIssueIDPattern.MatchString(value)
}

// invalidBeadID names the field holding an unsafe Bead ID.
func invalidBeadID(field, value string) error {
	return fmt.Errorf("%w: %s %q must be a safe Beads issue id: a lowercase prefix, a hyphen and an id, such as form-3yd.4", ErrInvalidBeadID, field, value)
}
