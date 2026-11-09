package proto

// CodexPermissions describes approval and sandbox independently. Authoritative is
// false for rollout history; Choices is empty when this route cannot change them.
type CodexPermissions struct {
	ApprovalPolicy string   `json:"approval_policy,omitempty"`
	Sandbox        string   `json:"sandbox,omitempty"`
	NetworkAccess  bool     `json:"network_access"`
	Reviewer       string   `json:"reviewer,omitempty"`
	Authoritative  bool     `json:"authoritative"`
	Choices        []string `json:"choices"`
	Reason         string   `json:"reason,omitempty"`
}
