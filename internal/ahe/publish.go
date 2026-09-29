package ahe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// Prepared contains a locally validated result and fresh, pinned native sources.
// It does not carry approval to admit a result.
type Prepared struct {
	receipt validation.Receipt
	sources []Record
	parents []string
}

func Prepare(ctx context.Context, query *Profile, authorityRaw []byte, expectedHash, requestID string, packageRaw []byte) (*Prepared, error) {
	a, err := validation.NewAuthority(authorityRaw, expectedHash)
	if err != nil {
		return nil, err
	}
	receipt, err := a.Validate(ctx, requestID, packageRaw)
	if err != nil {
		return nil, err
	}
	p := &Prepared{receipt: receipt, parents: a.SourceIDs()}
	for _, id := range p.parents {
		record, err := ReadRecord(ctx, query, id)
		if err != nil {
			return nil, err
		}
		want, _ := a.SourceDigest(id)
		if record.StatementSHA256 != want {
			return nil, fmt.Errorf("source statement binding mismatch: %s", id)
		}
		p.sources = append(p.sources, record)
	}
	return p, nil
}
func (p *Prepared) ready() bool {
	return p != nil && p.receipt.SchemaVersion == validation.Version && p.receipt.PackageSHA256 != "" && len(p.parents) > 0 && len(p.parents) == len(p.sources)
}

func (p *Prepared) Receipt() validation.Receipt { return p.receipt }
func (p *Prepared) Statement() string           { return p.receipt.Statement() }
func (p *Prepared) Sources() []Record {
	out := slices.Clone(p.sources)
	for i := range out {
		out[i].Raw = bytes.Clone(out[i].Raw)
	}
	return out
}

type Derivation struct {
	ParentNodeIDs []string `json:"parent_node_ids"`
	Method        string   `json:"method"`
	Producer      string   `json:"producer"`
	TraceRef      string   `json:"trace_ref"`
}
type ReviewRequest struct {
	Kind                 string     `json:"kind"`
	ProposalOccurrenceID string     `json:"proposal_occurrence_id"`
	Derivation           Derivation `json:"derivation"`
}

// Review exposes the exact native display for human inspection. The private
// binding prevents callers changing its subject, request or package accidentally.
type Review struct {
	Subject       string          `json:"subject"`
	DisplaySHA256 string          `json:"display_sha256"`
	PackageSHA256 string          `json:"package_sha256"`
	Raw           json.RawMessage `json:"raw"`
	request       ReviewRequest
	display       json.RawMessage
	prepared      *Prepared
}

func (p *Prepared) Review(ctx context.Context, endpoints *Profile, proposalID string) (Review, error) {
	var r Review
	if !p.ready() {
		return r, errors.New("validated prepared result required")
	}
	if endpoints == nil || endpoints.kind != ProfileEndpointReviewer {
		return r, ErrCapabilityUnavailable
	}
	if !strings.HasPrefix(proposalID, "occ:") || len(proposalID) > 200 {
		return r, errors.New("exact pending proposal ID required")
	}
	req := ReviewRequest{Kind: "derived_spec", ProposalOccurrenceID: proposalID, Derivation: Derivation{ParentNodeIDs: slices.Clone(p.parents), Method: "Pouch finite-model validation. This result is a model_conditional_conclusion, not an observed fact; every listed source is an AND basis. Source entailment and physical restore are not certified.", Producer: "pouch-finite-validation/v1", TraceRef: "sha256:" + p.receipt.PackageSHA256}}
	raw, err := endpoints.call(ctx, "get_endpoint_review", req)
	if err != nil {
		return r, err
	}
	var out struct {
		Subject   string `json:"subject"`
		Lifecycle struct {
			Mode string `json:"mode"`
		} `json:"lifecycle"`
		Display json.RawMessage `json:"display"`
	}
	if json.Unmarshal(raw, &out) != nil || out.Subject == "" || out.Lifecycle.Mode != "pending_admission" {
		return r, errors.New("endpoint is not pending an exact new review")
	}
	var display struct {
		ContractVersion string        `json:"contract_version"`
		Request         ReviewRequest `json:"request"`
		Proposal        struct {
			ProposalOccurrenceID string
			StatementText        string
			AdmissionOutcome     string
		} `json:"proposal"`
	}
	if json.Unmarshal(out.Display, &display) != nil || display.ContractVersion != "endpoint-review/v1" || !reflect.DeepEqual(display.Request, req) || display.Proposal.ProposalOccurrenceID != proposalID || display.Proposal.StatementText != p.Statement() || display.Proposal.AdmissionOutcome != "pending" {
		return r, errors.New("endpoint review does not bind the exact validated result")
	}
	return Review{Subject: out.Subject, DisplaySHA256: validation.Digest(out.Display), PackageSHA256: p.receipt.PackageSHA256, Raw: bytes.Clone(raw), request: req, display: bytes.Clone(out.Display), prepared: p}, nil
}

// Approval must come from the caller after showing the complete unchanged display.
// Pouch does not invent an approver or infer approval from a valid SAT assignment.
type Approval struct {
	RequestID     string `json:"request_id"`
	Subject       string `json:"subject"`
	DisplaySHA256 string `json:"display_sha256"`
	PackageSHA256 string `json:"package_sha256"`
	Reason        string `json:"reason"`
}
type Publication struct {
	Validation  validation.Receipt `json:"validation"`
	Admission   json.RawMessage    `json:"admission"`
	Readback    json.RawMessage    `json:"readback"`
	CanonicalID string             `json:"canonical_id"`
}
type nativeReceipt struct {
	ContractVersion string `json:"contract_version"`
	RequestID       string `json:"request_id"`
	ReviewSubject   string `json:"review_subject"`
	ReviewerID      string `json:"reviewer_id"`
	DecisionReason  string `json:"decision_reason"`
	Admission       struct {
		ProposalOccurrenceID string
		AdmissionOutcome     string
		CanonicalRef         string
		ParentNodeIDs        []string
	} `json:"admission"`
}

func (p *Prepared) Admit(ctx context.Context, endpoints, query *Profile, r Review, approval Approval) (Publication, error) {
	var publication Publication
	if !p.ready() {
		return publication, errors.New("validated prepared result required")
	}
	if endpoints == nil || endpoints.kind != ProfileEndpointReviewer || query == nil || query.kind != ProfileQuery {
		return publication, ErrCapabilityUnavailable
	}
	// Re-decode the stored native response; exported display fields are not authority.
	var native struct {
		Subject string          `json:"subject"`
		Display json.RawMessage `json:"display"`
	}
	if r.prepared != p || json.Unmarshal(r.Raw, &native) != nil || native.Subject != r.Subject || !bytes.Equal(native.Display, r.display) || r.DisplaySHA256 != validation.Digest(r.display) || r.PackageSHA256 != p.receipt.PackageSHA256 || approval.Subject != r.Subject || approval.DisplaySHA256 != r.DisplaySHA256 || approval.PackageSHA256 != r.PackageSHA256 || strings.TrimSpace(approval.Reason) == "" || len(approval.Reason) > 2000 || strings.TrimSpace(approval.RequestID) == "" || len(approval.RequestID) > 200 {
		return publication, errors.New("approval does not bind the complete unchanged review and package")
	}
	// Recheck source identities just before submission. No claim of a global currentness cut.
	for _, source := range p.sources {
		now, err := ReadRecord(ctx, query, source.CanonicalID)
		if err != nil {
			return publication, err
		}
		if now.StatementSHA256 != source.StatementSHA256 {
			return publication, errors.New("source binding changed before admission")
		}
	}
	raw, err := endpoints.call(ctx, "admit_reviewed_endpoint", map[string]any{"request_id": approval.RequestID, "review": r.request, "expected_subject": r.Subject, "decision": "approved", "decision_reason": approval.Reason})
	if err != nil {
		return publication, err
	}
	publication.Validation = p.receipt
	publication.Admission = bytes.Clone(raw)
	var receipt nativeReceipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.ContractVersion != "reviewed-endpoint-admission/v1" || receipt.RequestID != approval.RequestID || receipt.ReviewSubject != r.Subject || receipt.DecisionReason != approval.Reason || receipt.ReviewerID == "" || receipt.Admission.ProposalOccurrenceID != r.request.ProposalOccurrenceID || receipt.Admission.AdmissionOutcome != "admitted" || !strings.HasPrefix(receipt.Admission.CanonicalRef, "canon-node:") || !slices.Equal(receipt.Admission.ParentNodeIDs, p.parents) {
		return publication, fmt.Errorf("%w: admission receipt binding mismatch", ErrDeliveryUnknown)
	}
	publication.CanonicalID = receipt.Admission.CanonicalRef
	record, err := ReadRecord(ctx, query, publication.CanonicalID)
	if err != nil {
		return publication, fmt.Errorf("admitted result readback incomplete: %w", err)
	}
	publication.Readback = record.Raw
	var back struct {
		Endpoint  nativeReceipt `json:"endpoint_admission"`
		Canonical struct {
			NodeKind string `json:"node_kind"`
		} `json:"canonical"`
	}
	if json.Unmarshal(record.Raw, &back) != nil || record.Statement != p.Statement() || back.Canonical.NodeKind != "derived_claim" || !reflect.DeepEqual(back.Endpoint, receipt) {
		return publication, errors.New("admitted result readback binding mismatch")
	}
	return publication, nil
}

// Pending records the two explicit intake writes. Failure after the first write
// can leave a source without a proposal; callers retain receipts and reconcile.
type Pending struct {
	ProposalID      string          `json:"proposal_id"`
	SourceReceipt   json.RawMessage `json:"source_receipt"`
	ProposalReceipt json.RawMessage `json:"proposal_receipt"`
}

func (p *Prepared) Submit(ctx context.Context, intake *Profile, requestID string, observedAt time.Time) (Pending, error) {
	var pending Pending
	if !p.ready() {
		return pending, errors.New("validated prepared result required")
	}
	if intake == nil || intake.kind != ProfileIntake {
		return pending, ErrCapabilityUnavailable
	}
	if requestID == "" || len(requestID) > 180 || observedAt.IsZero() {
		return pending, errors.New("bounded intake request ID and explicit observation time required")
	}
	statement := p.Statement()
	raw, err := intake.call(ctx, "submit_external_source", map[string]any{"schema_version": "external-source-envelope-v1", "request_id": requestID + "/source", "source_system": "pouch", "source_namespace": "validated-model-conclusions", "object_type": "model_conditional_conclusion", "object_id": p.receipt.PackageSHA256, "revision": p.receipt.AuthoritySHA256, "source_location": "urn:pouch:sha256:" + p.receipt.PackageSHA256, "title": "Pouch conditional model conclusion", "content_format": "text/plain", "content_fidelity": "verbatim", "content": statement, "coverage": "full_document", "limitations": []string{}, "collector_id": "pouch", "connector_id": "pouch-public-mcp/v1", "observed_at": observedAt.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return pending, err
	}
	pending.SourceReceipt = bytes.Clone(raw)
	var source struct {
		SnapshotID string `json:"source_snapshot_id"`
		ViewID     string `json:"extraction_view_id"`
		Hash       string `json:"raw_content_hash"`
		Spans      []struct {
			ID string `json:"span_id"`
		} `json:"spans"`
	}
	if json.Unmarshal(raw, &source) != nil || source.SnapshotID == "" || source.ViewID == "" || source.Hash != "sha256:"+validation.Digest([]byte(statement)) || len(source.Spans) != 1 || source.Spans[0].ID == "" {
		return pending, fmt.Errorf("%w: source intake receipt mismatch", ErrDeliveryUnknown)
	}
	raw, err = intake.call(ctx, "submit_extractor_output", map[string]any{"request_id": requestID + "/proposal", "source_snapshot_id": source.SnapshotID, "extraction_view_id": source.ViewID, "extractor_definition": map[string]string{"name": "pouch-exact-validated-result", "version": "v1"}, "extractor_output": map[string]any{"proposals": []any{map[string]any{"proposal_local_id": "result", "statement_text": statement, "evidence_refs": []string{source.Spans[0].ID}}}}})
	if err != nil {
		return pending, err
	}
	pending.ProposalReceipt = bytes.Clone(raw)
	var proposal struct {
		ID     string `json:"proposal_occurrence_id"`
		Count  int    `json:"proposal_count"`
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &proposal) != nil || !strings.HasPrefix(proposal.ID, "occ:") || proposal.Count != 1 || proposal.Status != "pending" {
		return pending, fmt.Errorf("%w: proposal receipt mismatch", ErrDeliveryUnknown)
	}
	pending.ProposalID = proposal.ID
	return pending, nil
}
