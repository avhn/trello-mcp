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
	if status == 401 {
		return fmt.Errorf("trello API: HTTP 401; token/permission issue, not server policy; possible causes include an expired or revoked token, missing required scope (especially write scope), or no access to this object. If workspace_find {} succeeds, the token is valid for that read; check object access or write scope. If reads fail, check that the API key and token match, token expiry or revocation, and read permission. If renewal is needed, the human must privately run the installer's quoted absolute-path auth command or update Desktop extension settings, then reconnect or restart the MCP client. Never share keys, tokens or authorization URLs in chat or agent-captured commands")
	}
	if status == 403 {
		return fmt.Errorf("trello API: HTTP 403; token/permission issue, not server policy; check object access, granted permissions and token scopes")
	}
	return fmt.Errorf("trello API: HTTP %d", status)
}
