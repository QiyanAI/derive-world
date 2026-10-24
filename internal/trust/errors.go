// SPDX-License-Identifier: Apache-2.0
package trust

import "errors"

// Trust-domain errors are mapped to the frozen HTTP error envelope by the
// composition root. They never carry private content.
var (
	ErrNotFound             = errors.New("trust resource not found")
	ErrForbidden            = errors.New("trust permission denied")
	ErrInvalidRequest       = errors.New("invalid trust request")
	ErrConflict             = errors.New("trust conflict")
	ErrVersionConflict      = errors.New("trust version conflict")
	ErrConfirmationExpired  = errors.New("confirmation expired")
	ErrNoActiveDelegation   = errors.New("no active AI delegation")
	ErrPayloadTooLarge      = errors.New("trust payload too large")
	ErrProfileNotOwned      = errors.New("personal AI profile not owned by caller")
	ErrDelegationNotActive  = errors.New("delegation is not active")
	ErrUnsupportedAction    = errors.New("unsupported confirmation action")
	ErrConfirmationNotOwned = errors.New("confirmation not owned by caller")
)
