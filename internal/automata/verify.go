package automata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/Yui-Qi-Tang/calx/automaton"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// ReceiptVersion identifies the verification receipt schema.
const ReceiptVersion = "pouch-automaton-receipt/v1"

// Receipt is a conditional model conclusion. It never authorizes admission or
// execution, and it does not recheck the saved CNF, assignments or proofs.
type Receipt struct {
	SchemaVersion   string `json:"schema_version"`
	RequestID       string `json:"request_id"`
	AuthoritySHA256 string `json:"authority_sha256"`
	BundleSHA256    string `json:"bundle_sha256"`
	Status          string `json:"status"`
	Complete        bool   `json:"complete"`
	// Nil when there is no forward run.
	ForwardCost    *int   `json:"forward_cost"`
	RecoveryStatus string `json:"recovery_status"`
	ReturnSteps    *int   `json:"return_steps"`
	Reason         string `json:"reason,omitempty"`
	// True when walking the only run confirmed the length claims. False when
	// they rest on the recorded verified queries, as with choices.
	WalkChecked bool `json:"walk_checked"`
}

// Verify recomputes a saved bundle from the caller's authority and rejects it
// unless every conclusion matches. expected is the caller's authority digest, so
// a bundle cannot choose what it is checked against.
func Verify(ctx context.Context, a *Authority, expected, requestID string, raw []byte) (Receipt, error) {
	var saved Bundle
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&saved); err != nil {
		return Receipt{}, reject("BUNDLE_SCHEMA")
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return Receipt{}, reject("BUNDLE_SCHEMA")
	}
	if saved.SchemaVersion != BundleVersion {
		return Receipt{}, reject("BUNDLE_SCHEMA")
	}
	if a.RequestID() != requestID || saved.RequestID != requestID || saved.AuthoritySHA256 != expected || saved.Sources.AuthoritySHA256 != expected || saved.Sources.RequestID != requestID {
		return Receipt{}, reject("BUNDLE_BINDING")
	}
	if saved.Search.Policy != "shortest-run/"+automaton.Profile || saved.Search.Options.MaxOrbitSteps < 1 {
		return Receipt{}, reject("BUNDLE_SEARCH")
	}
	recomputed, err := Certify(ctx, a, expected, saved.Sources, saved.Search)
	var rejection *validation.Rejection
	if recomputed.Status == "REJECTED" || errors.As(err, &rejection) {
		return Receipt{}, reject("BUNDLE_CLAIM_REJECTED")
	}
	if err != nil {
		return Receipt{}, err
	}
	recomputed.ProjectVersion = saved.ProjectVersion
	want, err := json.Marshal(recomputed)
	if err != nil {
		return Receipt{}, err
	}
	got, err := json.Marshal(saved)
	if err != nil {
		return Receipt{}, err
	}
	if !bytes.Equal(want, got) {
		return Receipt{}, reject("BUNDLE_CLAIM_MISMATCH")
	}
	receipt := Receipt{SchemaVersion: ReceiptVersion, RequestID: requestID, AuthoritySHA256: expected, BundleSHA256: validation.Digest(raw), Status: saved.Status, Complete: saved.Complete, RecoveryStatus: saved.RecoveryStatus, ReturnSteps: saved.ReturnSteps, Reason: saved.Reason, WalkChecked: saved.Search.Deterministic}
	if saved.Search.Forward != nil {
		cost := len(saved.Search.Forward.Choices)
		receipt.ForwardCost = &cost
	}
	return receipt, nil
}
