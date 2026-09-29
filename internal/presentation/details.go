package presentation

import (
	"encoding/json"
	"slices"
	"sort"

	"github.com/Yui-Qi-Tang/pouch/internal/run"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// These fields are a display projection, never additional validation evidence.
type viewerDetails struct {
	TopologySupplied bool                   `json:"topology_supplied"`
	Assumptions      []declaredAssumption   `json:"assumptions"`
	ActionBasis      map[string][]string    `json:"action_basis"`
	SourceNames      map[string][]string    `json:"source_names"`
	OtherAssumptions map[string]string      `json:"other_assumptions"`
	Notices          []string               `json:"notices"`
	Paths            map[string]pathDetails `json:"paths"`
}

type queryDetails struct {
	Start validation.State `json:"start"`
	Goal  validation.State `json:"goal"`
}

type pathDetails struct {
	Scenario          string               `json:"scenario"`
	Note              string               `json:"note"`
	Forward           queryDetails         `json:"forward"`
	Return            queryDetails         `json:"return"`
	ForwardCost       *int                 `json:"forward_cost"`
	ReturnCost        *int                 `json:"return_cost"`
	ReturnAssumptions []declaredAssumption `json:"return_assumptions"`
}

type declaredAssumption struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Statement string   `json:"statement"`
	Basis     []string `json:"basis"`
}

func describeBundle(bundle run.Bundle) viewerDetails {
	var declaredCase string
	d := viewerDetails{
		TopologySupplied: bundle.EvidenceMatrix != nil,
		Assumptions:      []declaredAssumption{},
		ActionBasis:      map[string][]string{},
		SourceNames:      map[string][]string{},
		OtherAssumptions: map[string]string{},
		Notices:          []string{},
	}
	if m := bundle.EvidenceMatrix; m != nil {
		var scope struct {
			Topology string `json:"topology"`
		}
		if json.Unmarshal(m.Metadata["scope"], &scope) == nil && (scope.Topology == "not_supplied" || scope.Topology == "unknown") {
			d.TopologySupplied = false
		}
	}
	for key, value := range bundle.Contract.Assumptions {
		d.OtherAssumptions[key] = value
	}
	if raw, ok := bundle.Contract.Assumptions["complete_original_declaration"]; ok {
		var original struct {
			Case        string               `json:"case"`
			Assumptions []declaredAssumption `json:"assumptions"`
			Actions     []struct {
				ID    string   `json:"id"`
				Basis []string `json:"basis"`
			} `json:"actions"`
		}
		if json.Unmarshal([]byte(raw), &original) != nil || original.Assumptions == nil || original.Actions == nil {
			d.Notices = append(d.Notices, "The original declaration could not be displayed as structured assumptions and action references; its exact text is retained below.")
		} else {
			declaredCase = original.Case
			d.Assumptions = original.Assumptions
			// Match by exact action ID. A reference is not a proof of source support.
			for _, action := range bundle.Contract.Model.Actions {
				for _, declared := range original.Actions {
					if declared.ID == action.ID {
						d.ActionBasis[action.ID] = append(d.ActionBasis[action.ID], declared.Basis...)
					}
				}
			}
			delete(d.OtherAssumptions, "complete_original_declaration")
		}
	}
	if raw, ok := bundle.Contract.Assumptions["current_native_source_mapping"]; ok {
		var names map[string]string
		if json.Unmarshal([]byte(raw), &names) != nil || names == nil {
			d.Notices = append(d.Notices, "The source-name mapping could not be displayed; its exact text is retained below.")
		} else {
			for name, id := range names {
				if _, bound := bundle.Contract.SourceBinding[id]; !bound {
					d.Notices = append(d.Notices, "Source alias "+name+" refers to an unbound ID "+id+"; it is not shown as a bound source.")
					continue
				}
				d.SourceNames[id] = append(d.SourceNames[id], name)
			}
			for id := range d.SourceNames {
				sort.Strings(d.SourceNames[id])
			}
			delete(d.OtherAssumptions, "current_native_source_mapping")
		}
	}
	sort.Strings(d.Notices)
	d.Paths = describePaths(bundle, d, declaredCase)
	return d
}

func describePaths(bundle run.Bundle, details viewerDetails, declaredCase string) map[string]pathDetails {
	result := make(map[string]pathDetails, len(bundle.Search.Forward))
	for _, path := range bundle.Search.Forward {
		view := pathDetails{
			Forward:           queryDetails{Start: bundle.Contract.Start, Goal: bundle.Contract.Goal},
			Return:            queryDetails{Goal: bundle.Contract.Baseline},
			ReturnAssumptions: []declaredAssumption{},
		}
		if len(path.Trace.States) > 0 {
			// Return always starts at the full endpoint, never the replay cursor.
			view.Return.Start = path.Trace.States[len(path.Trace.States)-1]
			view.Scenario = view.Return.Start["chain"]
		}
		// These are explanatory notes for the reviewed example's exact paths,
		// not inferred repair classifications or additional validation claims.
		if declaredCase == "django-13344" {
			switch {
			case view.Scenario == "original_inner" && slices.Equal(path.Trace.Actions, []string{"build_original_inner", "await_view_original_inner", "observe_original_inner"}):
				view.Note = "Changed middleware order (original_inner). This path does not preserve the original outer-order repair scenario."
			case view.Scenario == "candidate_outer" && slices.Equal(path.Trace.Actions, []string{"build_candidate_outer", "await_view_candidate_outer", "observe_candidate_outer"}):
				view.Note = "Declared candidate retaining the outer order (candidate_outer). The result covers the declared local model behavior, not full Django or official repair acceptance."
			}
		}
		for _, checked := range bundle.Paths {
			if checked.ID != path.ID {
				continue
			}
			if checked.Receipt != nil && checked.ForwardVerified {
				cost := checked.Receipt.ForwardCost
				view.ForwardCost = &cost
				if checked.RecoveryStatus == "RETURN_VERIFIED" && checked.Return != nil && checked.Receipt.ReturnKind == "return" {
					cost := checked.Receipt.ReturnCost
					view.ReturnCost = &cost
				}
			}
			if checked.Return != nil {
				refs := map[string]bool{}
				for _, action := range checked.Return.Actions {
					for _, ref := range details.ActionBasis[action] {
						refs[ref] = true
					}
				}
				for _, assumption := range details.Assumptions {
					if refs[assumption.ID] {
						view.ReturnAssumptions = append(view.ReturnAssumptions, assumption)
					}
				}
			}
			break
		}
		result[path.ID] = view
	}
	return result
}
