package drift

import (
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
)

func planWithDrift(changes ...*tfjson.ResourceChange) *tfjson.Plan {
	return &tfjson.Plan{ResourceDrift: changes}
}

func TestFromPlanCreate(t *testing.T) {
	result := FromPlan(planWithDrift(&tfjson.ResourceChange{
		Address: "aws_instance.web",
		Type:    "aws_instance",
		Change: &tfjson.Change{
			Actions: tfjson.Actions{tfjson.ActionCreate},
			Before:  nil,
			After:   map[string]any{"id": "i-123"},
		},
	}))

	if result.Summary.Added != 1 || result.Summary.Changed != 0 || result.Summary.Destroyed != 0 {
		t.Fatalf("summary = %+v, want 1 added", result.Summary)
	}
	if got := result.Resources[0].Action; got != ActionCreate {
		t.Errorf("action = %q, want create", got)
	}
	if result.Resources[0].After["id"] != "i-123" {
		t.Errorf("after[id] = %v, want i-123", result.Resources[0].After["id"])
	}
	if !result.Drifted() {
		t.Error("Drifted() = false, want true")
	}
}

func TestFromPlanUpdate(t *testing.T) {
	result := FromPlan(planWithDrift(&tfjson.ResourceChange{
		Address: "aws_instance.web",
		Type:    "aws_instance",
		Change: &tfjson.Change{
			Actions: tfjson.Actions{tfjson.ActionUpdate},
			Before:  map[string]any{"instance_type": "t2.micro"},
			After:   map[string]any{"instance_type": "t2.large"},
		},
	}))

	if result.Summary.Changed != 1 {
		t.Fatalf("summary = %+v, want 1 changed", result.Summary)
	}
	if result.Resources[0].Action != ActionUpdate {
		t.Errorf("action = %q, want update", result.Resources[0].Action)
	}
	if result.Resources[0].Before["instance_type"] != "t2.micro" {
		t.Errorf("before = %v", result.Resources[0].Before)
	}
	if result.Resources[0].After["instance_type"] != "t2.large" {
		t.Errorf("after = %v", result.Resources[0].After)
	}
}

func TestFromPlanDestroy(t *testing.T) {
	result := FromPlan(planWithDrift(&tfjson.ResourceChange{
		Address: "aws_instance.web",
		Type:    "aws_instance",
		Change: &tfjson.Change{
			Actions: tfjson.Actions{tfjson.ActionDelete},
			Before:  map[string]any{"id": "i-123"},
			After:   nil,
		},
	}))

	if result.Summary.Destroyed != 1 {
		t.Fatalf("summary = %+v, want 1 destroyed", result.Summary)
	}
	if result.Resources[0].Action != ActionDelete {
		t.Errorf("action = %q, want delete", result.Resources[0].Action)
	}
	if result.Resources[0].After != nil {
		t.Errorf("after = %v, want nil", result.Resources[0].After)
	}
}

func TestFromPlanReplace(t *testing.T) {
	result := FromPlan(planWithDrift(&tfjson.ResourceChange{
		Address: "aws_instance.web",
		Type:    "aws_instance",
		Change: &tfjson.Change{
			// Terraform represents a replace as delete-then-create (or
			// create-then-destroy) together in one Actions slice -
			// Actions.Replace() is what correctly recognizes this,
			// rather than hand-checking for exactly these two values.
			Actions: tfjson.Actions{tfjson.ActionDelete, tfjson.ActionCreate},
			Before:  map[string]any{"id": "i-123"},
			After:   map[string]any{"id": "i-456"},
		},
	}))

	if result.Summary.Changed != 1 {
		t.Fatalf("summary = %+v, want 1 changed (replace counts as changed)", result.Summary)
	}
	if result.Resources[0].Action != ActionReplace {
		t.Errorf("action = %q, want replace", result.Resources[0].Action)
	}
}

func TestFromPlanNoOp(t *testing.T) {
	result := FromPlan(planWithDrift(&tfjson.ResourceChange{
		Address: "aws_instance.web",
		Type:    "aws_instance",
		Change: &tfjson.Change{
			Actions: tfjson.Actions{tfjson.ActionNoop},
			Before:  map[string]any{"id": "i-123"},
			After:   map[string]any{"id": "i-123"},
		},
	}))

	if result.Summary.Unchanged != 1 {
		t.Fatalf("summary = %+v, want 1 unchanged", result.Summary)
	}
	if result.Drifted() {
		t.Error("Drifted() = true, want false (no-op only)")
	}
}

func TestFromPlanWhollySensitiveAttribute(t *testing.T) {
	result := FromPlan(planWithDrift(&tfjson.ResourceChange{
		Address: "aws_db_instance.main",
		Type:    "aws_db_instance",
		Change: &tfjson.Change{
			Actions:         tfjson.Actions{tfjson.ActionUpdate},
			Before:          map[string]any{"password": "old-real-password"},
			After:           map[string]any{"password": "new-real-password"},
			BeforeSensitive: map[string]any{"password": true},
			AfterSensitive:  map[string]any{"password": true},
		},
	}))

	rd := result.Resources[0]
	if rd.Before["password"] != sensitiveRedacted {
		t.Errorf("before[password] = %v, want redacted", rd.Before["password"])
	}
	if rd.After["password"] != sensitiveRedacted {
		t.Errorf("after[password] = %v, want redacted", rd.After["password"])
	}
	if !rd.HasSensitive {
		t.Error("HasSensitive = false, want true")
	}
	if rd.HasUnknown {
		t.Error("HasUnknown = true, want false")
	}
}

func TestFromPlanPartiallySensitiveNestedAttribute(t *testing.T) {
	result := FromPlan(planWithDrift(&tfjson.ResourceChange{
		Address: "kubernetes_secret.app",
		Type:    "kubernetes_secret",
		Change: &tfjson.Change{
			Actions: tfjson.Actions{tfjson.ActionUpdate},
			Before: map[string]any{
				"data": map[string]any{"username": "alice", "password": "real-secret"},
			},
			After: map[string]any{
				"data": map[string]any{"username": "alice", "password": "new-real-secret"},
			},
			// Only "password" is marked sensitive within the nested
			// "data" map - "username" has no entry, meaning not
			// sensitive, per tfjson's documented shape.
			BeforeSensitive: map[string]any{
				"data": map[string]any{"password": true},
			},
			AfterSensitive: map[string]any{
				"data": map[string]any{"password": true},
			},
		},
	}))

	rd := result.Resources[0]
	data, ok := rd.After["data"].(map[string]any)
	if !ok {
		t.Fatalf("after[data] is not a map: %#v", rd.After["data"])
	}
	if data["username"] != "alice" {
		t.Errorf("after[data][username] = %v, want alice (not sensitive, should be untouched)", data["username"])
	}
	if data["password"] != sensitiveRedacted {
		t.Errorf("after[data][password] = %v, want redacted", data["password"])
	}
	if !rd.HasSensitive {
		t.Error("HasSensitive = false, want true")
	}
}

func TestFromPlanUnknownUntilApplyAttribute(t *testing.T) {
	result := FromPlan(planWithDrift(&tfjson.ResourceChange{
		Address: "aws_instance.web",
		Type:    "aws_instance",
		Change: &tfjson.Change{
			Actions:      tfjson.Actions{tfjson.ActionUpdate},
			Before:       map[string]any{"ami": "ami-old"},
			After:        map[string]any{"ami": "ami-old", "private_ip": nil},
			AfterUnknown: map[string]any{"private_ip": true},
		},
	}))

	rd := result.Resources[0]
	if rd.After["private_ip"] != unknownRedacted {
		t.Errorf("after[private_ip] = %v, want %q", rd.After["private_ip"], unknownRedacted)
	}
	if rd.After["ami"] != "ami-old" {
		t.Errorf("after[ami] = %v, want ami-old (untouched)", rd.After["ami"])
	}
	if !rd.HasUnknown {
		t.Error("HasUnknown = false, want true")
	}
	if rd.HasSensitive {
		t.Error("HasSensitive = true, want false")
	}
}

func TestFromPlanAggregatesSummaryAcrossResources(t *testing.T) {
	result := FromPlan(planWithDrift(
		&tfjson.ResourceChange{
			Address: "a", Type: "x",
			Change: &tfjson.Change{Actions: tfjson.Actions{tfjson.ActionCreate}, After: map[string]any{}},
		},
		&tfjson.ResourceChange{
			Address: "b", Type: "x",
			Change: &tfjson.Change{Actions: tfjson.Actions{tfjson.ActionUpdate}, Before: map[string]any{}, After: map[string]any{}},
		},
		&tfjson.ResourceChange{
			Address: "c", Type: "x",
			Change: &tfjson.Change{Actions: tfjson.Actions{tfjson.ActionDelete}, Before: map[string]any{}},
		},
		&tfjson.ResourceChange{
			Address: "d", Type: "x",
			Change: &tfjson.Change{Actions: tfjson.Actions{tfjson.ActionNoop}, Before: map[string]any{}, After: map[string]any{}},
		},
	))

	want := Summary{Added: 1, Changed: 1, Destroyed: 1, Unchanged: 1}
	if result.Summary != want {
		t.Errorf("summary = %+v, want %+v", result.Summary, want)
	}
	if len(result.Resources) != 4 {
		t.Errorf("len(resources) = %d, want 4", len(result.Resources))
	}
}

func TestFromPlanSkipsNilChange(t *testing.T) {
	result := FromPlan(planWithDrift(&tfjson.ResourceChange{Address: "a", Change: nil}))
	if len(result.Resources) != 0 {
		t.Errorf("len(resources) = %d, want 0 (nil Change should be skipped, not panic)", len(result.Resources))
	}
}
