package trello

import "fmt"

// PolicyDenied distinguishes local safety refusals from upstream authentication errors.
// Reasons may identify caller-supplied objects or configured selectors, never resolved denied scopes.
func PolicyDenied(policy, reason string) error {
	return fmt.Errorf("access denied by %s: %s. This is server policy, not a token/permission error. Call server_health with detail \"policy\" for details", policy, reason)
}

// OwnershipDenied fails closed when an object's current parent cannot be proven.
func OwnershipDenied(object string) error {
	return ownershipDenial{PolicyDenied("ownership verification", object+" ownership could not be verified, so policy refused access; verify the object id and parent constraints")}
}

type ownershipDenial struct{ error }

// httpError never includes upstream bodies, URLs or credentials.
func httpError(status int) error {
	if status == 401 || status == 403 {
		return fmt.Errorf("trello API: HTTP %d; token/permission issue, not server policy; check the Trello token and object permissions", status)
	}
	return fmt.Errorf("trello API: HTTP %d", status)
}
