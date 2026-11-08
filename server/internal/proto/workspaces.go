package proto

// Workspace describes a host space independently of the agents running in it.
// Node IDs are host-local; the hub qualifies them by machine and source.
type Workspace struct {
	ID        string `json:"id"`
	MachineID string `json:"machine_id,omitempty"`
	Source    string `json:"source"`
	SourceID  string `json:"source_id"`
	// ParentSourceID names a workspace on the same machine and source, even without agents.
	ParentSourceID string `json:"parent_source_id,omitempty"`
	Index          int    `json:"index"`
	Title          string `json:"title"`
	Cwd            string `json:"cwd"`
	Branch         string `json:"branch,omitempty"`
	Online         bool   `json:"online"`
	Available      bool   `json:"available"`
	LastSeen       string `json:"last_seen,omitempty"`
}

type WorkspacesResponse struct {
	Workspaces []Workspace            `json:"workspaces"`
	Errors     []WorkspaceSourceError `json:"errors,omitempty"`
}

// WorkspaceSourceError identifies a host whose catalog could not be refreshed.
// Other sources in the same response may have succeeded, even with no spaces.
type WorkspaceSourceError struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}
