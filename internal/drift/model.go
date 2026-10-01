// Package drift turns a raw *tfjson.Plan from a refresh-only Terraform
// plan into groundtruth's own, already-redacted representation - the
// only form of a plan that's ever persisted or returned by the API.
package drift

// Action is a simplified classification of what happened to a resource,
// derived from tfjson.Actions (which can be a sequence like
// ["delete","create"] for a replace).
type Action string

// The possible values of Action.
const (
	ActionNoOp    Action = "no-op"
	ActionCreate  Action = "create"
	ActionUpdate  Action = "update"
	ActionDelete  Action = "delete"
	ActionReplace Action = "replace"
)

// ResourceDrift describes one resource's drift, with Before/After
// already redacted - sensitive values replaced with a fixed placeholder,
// and values that won't be known until apply replaced with another.
// Neither Before nor After here ever holds a real value Terraform marked
// sensitive or unknown.
type ResourceDrift struct {
	Address       string         `json:"address"`
	Type          string         `json:"type"`
	ModuleAddress string         `json:"module_address,omitempty"`
	Action        Action         `json:"action"`
	Before        map[string]any `json:"before,omitempty"`
	After         map[string]any `json:"after,omitempty"`
	HasSensitive  bool           `json:"has_sensitive"`
	HasUnknown    bool           `json:"has_unknown"`
}

// Summary is an aggregate count across every resource in a Result.
type Summary struct {
	Added     int `json:"added"`
	Changed   int `json:"changed"`
	Destroyed int `json:"destroyed"`
	Unchanged int `json:"unchanged"`
}

// Result is the fully-redacted outcome of one drift check - this, not
// the *tfjson.Plan it was built from, is what's safe to store or return.
type Result struct {
	Summary   Summary         `json:"summary"`
	Resources []ResourceDrift `json:"resources"`
}

// Drifted reports whether any resource actually changed. A check can
// succeed and still have Drifted == false (nothing changed since last
// time).
func (r Result) Drifted() bool {
	return r.Summary.Added > 0 || r.Summary.Changed > 0 || r.Summary.Destroyed > 0
}
