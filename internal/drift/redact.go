package drift

import tfjson "github.com/hashicorp/terraform-json"

// sensitiveRedacted and unknownRedacted replace any value Terraform
// marked sensitive or not-yet-known, anywhere in Before/After, before
// the result is ever persisted or returned by the API.
const (
	sensitiveRedacted = "(sensitive value)"
	unknownRedacted   = "(known after apply)"
)

// FromPlan converts a raw, unredacted *tfjson.Plan into a Result. It
// reads plan.ResourceDrift specifically - not plan.ResourceChanges -
// since for a `-refresh-only` plan that's the field Terraform documents
// as "the changes detected when it compared the most recent state to
// the prior saved state," which is exactly what groundtruth means by
// drift. resource_changes instead describes what Terraform would do to
// match *configuration*, a different question this tool never asks.
func FromPlan(plan *tfjson.Plan) Result {
	var result Result

	for _, rc := range plan.ResourceDrift {
		if rc == nil || rc.Change == nil {
			continue
		}

		rd := buildResourceDrift(rc)
		result.Resources = append(result.Resources, rd)

		switch rd.Action {
		case ActionCreate:
			result.Summary.Added++
		case ActionUpdate, ActionReplace:
			result.Summary.Changed++
		case ActionDelete:
			result.Summary.Destroyed++
		case ActionNoOp:
			result.Summary.Unchanged++
		}
	}

	return result
}

func buildResourceDrift(rc *tfjson.ResourceChange) ResourceDrift {
	change := rc.Change

	before, _ := redactValue(change.Before, change.BeforeSensitive, nil).(map[string]any)
	after, _ := redactValue(change.After, change.AfterSensitive, change.AfterUnknown).(map[string]any)

	return ResourceDrift{
		Address:       rc.Address,
		Type:          rc.Type,
		ModuleAddress: rc.ModuleAddress,
		Action:        classifyAction(change.Actions),
		Before:        before,
		After:         after,
		HasSensitive:  anyMarked(change.BeforeSensitive) || anyMarked(change.AfterSensitive),
		HasUnknown:    anyMarked(change.AfterUnknown),
	}
}

func classifyAction(actions tfjson.Actions) Action {
	switch {
	case actions.Replace():
		return ActionReplace
	case actions.Delete():
		return ActionDelete
	case actions.Create():
		return ActionCreate
	case actions.Update():
		return ActionUpdate
	default:
		return ActionNoOp
	}
}

// redactValue walks value in lockstep with sensitiveMarker/unknownMarker
// - tfjson gives these the same shape as value itself (object mirrors
// object, array mirrors array), with `true` at a leaf, or at the root of
// an entire subtree, wherever Terraform marked that value sensitive or
// not-yet-known. A marker of `true` at any level redacts everything
// beneath it in one step; otherwise each key/index recurses with its own
// corresponding marker.
func redactValue(value, sensitiveMarker, unknownMarker any) any {
	if marked, ok := sensitiveMarker.(bool); ok && marked {
		return sensitiveRedacted
	}
	if marked, ok := unknownMarker.(bool); ok && marked {
		return unknownRedacted
	}

	switch v := value.(type) {
	case map[string]any:
		sensitiveMap, _ := sensitiveMarker.(map[string]any)
		unknownMap, _ := unknownMarker.(map[string]any)
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[k] = redactValue(val, sensitiveMap[k], unknownMap[k])
		}
		return out

	case []any:
		sensitiveSlice, _ := sensitiveMarker.([]any)
		unknownSlice, _ := unknownMarker.([]any)
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = redactValue(val, indexOrNil(sensitiveSlice, i), indexOrNil(unknownSlice, i))
		}
		return out

	default:
		return v
	}
}

func indexOrNil(s []any, i int) any {
	if i < len(s) {
		return s[i]
	}
	return nil
}

// anyMarked reports whether marker (a sensitive/unknown tree in the same
// shape tfjson uses) is `true` anywhere within it.
func anyMarked(marker any) bool {
	switch m := marker.(type) {
	case bool:
		return m
	case map[string]any:
		for _, v := range m {
			if anyMarked(v) {
				return true
			}
		}
	case []any:
		for _, v := range m {
			if anyMarked(v) {
				return true
			}
		}
	}
	return false
}
