package hosted

// WastelandConfig describes one connected wasteland in hosted responses.
type WastelandConfig struct {
	Upstream string `json:"upstream"`
	ForkOrg  string `json:"fork_org"`
	ForkDB   string `json:"fork_db"`
	Mode     string `json:"mode"`
	Signing  bool   `json:"signing"`
}

// authStatusResponse is the JSON response for GET /api/auth/status.
type authStatusResponse struct {
	Authenticated bool              `json:"authenticated"`
	Connected     bool              `json:"connected"`
	RigHandle     string            `json:"rig_handle,omitempty"`
	Wastelands    []WastelandConfig `json:"wastelands,omitempty"`
	Environment   string            `json:"environment,omitempty"`
}
