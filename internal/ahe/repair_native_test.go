package ahe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// Opt-in fixture orchestration only. Ordinary tests do not write native data.
// Public MCP is the only path used for evidence intake/admission/readback.
func repairCall(t *testing.T, cfg nativeConfig, p *Profile, tool string, args any) json.RawMessage {
	t.Helper()
	raw, err := p.call(t.Context(), tool, args)
	record := map[string]any{"tool": tool, "arguments": args, "response": raw}
	if err != nil {
		record["error"] = err.Error()
		if te, ok := err.(*ToolError); ok {
			record["error_payload"] = te.Payload
		}
	}
	nativeWrite(t, filepath.Join(cfg.Root, "repair", fmt.Sprintf("call-%06d.json", nativeTraceSequence.Add(1))), record)
	if err != nil {
		t.Fatalf("native %s failed (raw error retained): %v", tool, err)
	}
	return raw
}

type repairPending struct {
	Attempt, ID         string
	Submitted, Proposed json.RawMessage
}

func submitRepairStatement(t *testing.T, cfg nativeConfig, intake *Profile, key, statement string) repairPending {
	t.Helper()
	submitted := repairCall(t, cfg, intake, "submit_external_source", map[string]any{
		"schema_version": "external-source-envelope-v1", "request_id": cfg.RunID + "/" + key + "/source", "source_system": "pouch-native-test", "source_namespace": cfg.RunID, "object_type": "recorded_repair_evidence", "object_id": key, "revision": validation.Digest([]byte(statement)), "source_location": "urn:pouch:sha256:" + validation.Digest([]byte(statement)), "title": "TEST FIXTURE: bound Django 13344 repair evidence", "content_format": "text/plain", "content_fidelity": "verbatim", "content": statement, "coverage": "full_document", "limitations": []string{}, "collector_id": "pouch-native-test", "connector_id": "pouch-native-test", "observed_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	var source struct {
		Snapshot string `json:"source_snapshot_id"`
		View     string `json:"extraction_view_id"`
		Hash     string `json:"raw_content_hash"`
		Spans    []struct {
			ID string `json:"span_id"`
		} `json:"spans"`
	}
	repairDecode(t, submitted, &source)
	if source.Hash != "sha256:"+validation.Digest([]byte(statement)) || source.Snapshot == "" || source.View == "" || len(source.Spans) == 0 || len(source.Spans) > 128 {
		t.Fatal("native intake binding mismatch")
	}
	refs := []string{}
	for _, s := range source.Spans {
		refs = append(refs, s.ID)
	}
	proposed := repairCall(t, cfg, intake, "submit_extractor_output", map[string]any{"request_id": cfg.RunID + "/" + key + "/proposal", "source_snapshot_id": source.Snapshot, "extraction_view_id": source.View, "extractor_definition": map[string]string{"name": "pouch-exact-record-binding", "version": "v1"}, "extractor_output": map[string]any{"proposals": []any{map[string]any{"proposal_local_id": "record", "statement_text": statement, "evidence_refs": refs}}}})
	var proposal struct {
		Attempt string `json:"extraction_attempt_id"`
		ID      string `json:"proposal_occurrence_id"`
		Count   int    `json:"proposal_count"`
		Status  string `json:"status"`
	}
	repairDecode(t, proposed, &proposal)
	if proposal.Count != 1 || proposal.Status != "pending" || !strings.HasPrefix(proposal.ID, "occ:") {
		t.Fatal("native proposal mismatch")
	}
	return repairPending{Attempt: proposal.Attempt, ID: proposal.ID, Submitted: submitted, Proposed: proposed}
}

func admitRepairArtifact(t *testing.T, cfg nativeConfig, intake, reviewer, query *Profile, key, origin string, raw []byte) Record {
	t.Helper()
	envelope, err := encodeRepairArtifact(origin, raw)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRepairArtifact(envelope, origin, validation.Digest(raw))
	if err != nil || !bytes.Equal(decoded, raw) {
		t.Fatal("source encoding mismatch: ", err)
	}
	nativeWrite(t, filepath.Join(cfg.Root, "repair", key+"-envelope.json"), json.RawMessage(envelope))
	pending := submitRepairStatement(t, cfg, intake, key, string(envelope))
	review := repairCall(t, cfg, reviewer, "get_source_claim_review", map[string]string{"extraction_attempt_id": pending.Attempt, "proposal_occurrence_id": pending.ID})
	var reviewed struct {
		Subject json.RawMessage `json:"subject"`
		Display struct {
			Payload string `json:"payload_utf8"`
		} `json:"display"`
	}
	repairDecode(t, review, &reviewed)
	var payload struct {
		Basis struct {
			Statement string `json:"statement_text"`
			Proposal  string `json:"proposal_occurrence_id"`
		} `json:"proposal_basis"`
	}
	repairDecode(t, []byte(reviewed.Display.Payload), &payload)
	if len(reviewed.Subject) == 0 || payload.Basis.Statement != string(envelope) || payload.Basis.Proposal != pending.ID {
		t.Fatal("native source review mismatch")
	}
	admission := repairCall(t, cfg, reviewer, "admit_reviewed_source_claim", map[string]any{"extraction_attempt_id": pending.Attempt, "expected_subject": reviewed.Subject, "decision": "approved", "decision_reason": "TEST APPROVAL STUB: exact lossless synthetic/public fixture bytes only, not human approval or semantic certification."})
	var admitted struct {
		ID      string `json:"canonical_ref"`
		Outcome string `json:"admission_outcome"`
	}
	repairDecode(t, admission, &admitted)
	if admitted.Outcome != "admitted" {
		t.Fatal("source not admitted")
	}
	record, err := ReadRecord(t.Context(), query, admitted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.StatementSHA256 != validation.Digest(envelope) || record.Statement != string(envelope) {
		t.Fatal("native exact envelope readback mismatch")
	}
	decoded, err = decodeRepairArtifact([]byte(record.Statement), origin, validation.Digest(raw))
	if err != nil || !bytes.Equal(decoded, raw) {
		t.Fatal("native raw-byte readback mismatch: ", err)
	}
	nativeWrite(t, filepath.Join(cfg.Root, "repair", key+"-source.json"), map[string]any{"origin_id": origin, "raw_sha256": validation.Digest(raw), "envelope_statement_sha256": record.StatementSHA256, "pending": pending, "review": review, "admission": admission, "readback": record, "decoded_bytes_equal": true})
	return record
}

// This derived statement is a binding of existing evidence, not a new physical
// observation. Its model-validation parent remains conditional and unchanged.
func admitRepairBinding(t *testing.T, cfg nativeConfig, intake, endpoints, query *Profile, key, statement string, parents []Record) Record {
	t.Helper()
	ids := []string{}
	for _, p := range parents {
		ids = append(ids, p.CanonicalID)
	}
	slices.Sort(ids)
	pending := submitRepairStatement(t, cfg, intake, key, statement)
	req := ReviewRequest{Kind: "derived_spec", ProposalOccurrenceID: pending.ID, Derivation: Derivation{ParentNodeIDs: ids, Method: "Bind the independently validated model result to the pinned, previously recorded three-file replay. All parents are required. Hash/event consistency was checked by Pouch's fixed-example integration harness. No new execution, official test, global currentness or general physical rollback certification.", Producer: "pouch-recorded-repair-binding/v1", TraceRef: "sha256:" + validation.Digest([]byte(statement))}}
	review := repairCall(t, cfg, endpoints, "get_endpoint_review", req)
	var r struct {
		Subject   string `json:"subject"`
		Lifecycle struct {
			Mode string `json:"mode"`
		} `json:"lifecycle"`
		Display json.RawMessage `json:"display"`
	}
	repairDecode(t, review, &r)
	var display struct {
		Version  string        `json:"contract_version"`
		Request  ReviewRequest `json:"request"`
		Proposal struct {
			ID        string `json:"ProposalOccurrenceID"`
			Statement string `json:"StatementText"`
			Outcome   string `json:"AdmissionOutcome"`
		} `json:"proposal"`
	}
	repairDecode(t, r.Display, &display)
	if r.Subject == "" || r.Lifecycle.Mode != "pending_admission" || display.Version != "endpoint-review/v1" || !reflect.DeepEqual(display.Request, req) || display.Proposal.ID != pending.ID || display.Proposal.Statement != statement || display.Proposal.Outcome != "pending" {
		t.Fatal("execution binding review mismatch")
	}
	for _, parent := range parents {
		now, err := ReadRecord(t.Context(), query, parent.CanonicalID)
		if err != nil || now.StatementSHA256 != parent.StatementSHA256 {
			t.Fatal("execution parent changed: ", err)
		}
	}
	reason := "TEST APPROVAL STUB: exact recorded-repair binding and both AND parents in this disposable cluster only; not new execution or human certification."
	request := cfg.RunID + "/" + key + "/admit"
	approval := map[string]any{"request_id": request, "review": req, "expected_subject": r.Subject, "decision": "approved", "decision_reason": reason}
	admitted := repairCall(t, cfg, endpoints, "admit_reviewed_endpoint", approval)
	var receipt nativeReceipt
	repairDecode(t, admitted, &receipt)
	if receipt.ContractVersion != "reviewed-endpoint-admission/v1" || receipt.RequestID != request || receipt.ReviewSubject != r.Subject || receipt.DecisionReason != reason || receipt.ReviewerID == "" || receipt.Admission.ProposalOccurrenceID != pending.ID || receipt.Admission.AdmissionOutcome != "admitted" || !slices.Equal(receipt.Admission.ParentNodeIDs, ids) {
		t.Fatal("execution native receipt mismatch")
	}
	back, err := ReadRecord(t.Context(), query, receipt.Admission.CanonicalRef)
	if err != nil {
		t.Fatal(err)
	}
	var readback struct {
		Endpoint  nativeReceipt `json:"endpoint_admission"`
		Canonical struct {
			Kind string `json:"node_kind"`
		} `json:"canonical"`
	}
	repairDecode(t, back.Raw, &readback)
	if back.Statement != statement || readback.Canonical.Kind != "derived_claim" || !reflect.DeepEqual(readback.Endpoint, receipt) {
		t.Fatal("execution binding readback mismatch")
	}
	nativeWrite(t, filepath.Join(cfg.Root, "repair", key+"-publication.json"), map[string]any{"statement": statement, "statement_sha256": validation.Digest([]byte(statement)), "pending": pending, "review": review, "display_sha256": validation.Digest(r.Display), "approval": approval, "admission": admitted, "readback": back, "exact_parents": ids})
	return back
}

func TestNativeRepairPublication(t *testing.T) {
	cfg := nativeFixture(t)
	input := loadRepairInput(t, cfg.Project)
	dest := filepath.Join(cfg.Root, "repair")
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal("refusing repeated repair publication: ", err)
	}
	nativeWrite(t, filepath.Join(dest, "started.json"), map[string]any{"status": "STARTED", "run_id": cfg.RunID, "started_at": time.Now().UTC().Format(time.RFC3339Nano), "report_sha256": repairReport, "new_sat_queries": 0, "new_patch_operations": 0})
	q := nativeProfile(t, cfg, ProfileQuery)
	i := nativeProfile(t, cfg, ProfileIntake)
	s := nativeProfile(t, cfg, ProfileSourceClaimReviewer)
	e := nativeProfile(t, cfg, ProfileEndpointReviewer)
	original := input.Inputs.Authority.Contract()
	fresh := input.Inputs.Authority.Contract()
	fresh.RequestID = cfg.RunID + "/django-13344-repair"
	fresh.SourceBinding = map[string]string{}
	sourceMap := []map[string]string{}
	for n, source := range input.Inputs.Sources.Sources {
		record := admitRepairArtifact(t, cfg, i, s, q, fmt.Sprintf("source-%02d", n), source.ID, input.Inputs.Content[source.ID])
		fresh.SourceBinding[record.CanonicalID] = record.StatementSHA256
		for key, value := range fresh.Assumptions {
			fresh.Assumptions[key] = strings.ReplaceAll(value, source.ID, record.CanonicalID)
		}
		sourceMap = append(sourceMap, map[string]string{"original_id": source.ID, "original_sha256": source.SHA256, "canonical_id": record.CanonicalID, "envelope_statement_sha256": record.StatementSHA256})
	}
	reportSource := admitRepairArtifact(t, cfg, i, s, q, "execution-report", repairRun+"/replay/result.json", input.ReportRaw)
	// Explicitly undo the identity/transport rebind to prove all model semantics
	// and assumptions match; native IDs are aliases for verified original bytes.
	restored := fresh
	restored.RequestID = original.RequestID
	restored.SourceBinding = maps.Clone(original.SourceBinding)
	restored.Assumptions = maps.Clone(fresh.Assumptions)
	for _, row := range sourceMap {
		for k, v := range restored.Assumptions {
			restored.Assumptions[k] = strings.ReplaceAll(v, row["canonical_id"], row["original_id"])
		}
	}
	if !reflect.DeepEqual(restored, original) {
		t.Fatal("identity rebind changed the original model")
	}
	authorityRaw := repairJSON(t, fresh)
	authorityHash := validation.Digest(authorityRaw)
	nativeWrite(t, filepath.Join(dest, "authority.json"), fresh)
	nativeWrite(t, filepath.Join(dest, "source-map.json"), sourceMap)
	nativeWrite(t, filepath.Join(dest, "input-binding.json"), map[string]any{"original_authority_sha256": repairAuthority, "native_authority_sha256": authorityHash, "original_bundle_sha256": repairBundle, "original_report_sha256": repairReport, "original_manifest_sha256": repairManifest, "original_request_id": repairRequest, "native_request_id": fresh.RequestID, "source_map_sha256": validation.Digest(repairJSON(t, sourceMap)), "model_semantics_unchanged": true, "new_execution": false})
	rows := []map[string]any{}
	for n, raw := range input.Packages {
		id := input.Report.Paths[n].ID
		var pkg, old validation.Package
		repairDecode(t, raw, &pkg)
		repairDecode(t, raw, &old)
		pkg.RequestID = fresh.RequestID
		pkg.AuthoritySHA256 = authorityHash
		pkg.SourceBinding = maps.Clone(fresh.SourceBinding)
		pkg.Assumptions = maps.Clone(fresh.Assumptions)
		restored := pkg
		restored.RequestID = old.RequestID
		restored.AuthoritySHA256 = old.AuthoritySHA256
		restored.SourceBinding = old.SourceBinding
		restored.Assumptions = old.Assumptions
		if !reflect.DeepEqual(restored, old) {
			t.Fatal("package rebind changed traces")
		}
		rebound := repairJSON(t, pkg)
		nativeWrite(t, filepath.Join(dest, id+"-package.json"), pkg)
		prepared, err := Prepare(t.Context(), q, authorityRaw, authorityHash, fresh.RequestID, rebound)
		if err != nil {
			t.Fatal(err)
		}
		req := cfg.RunID + "/" + id + "/model"
		pending, err := prepared.Submit(t.Context(), i, req, time.Now().UTC())
		if err != nil {
			t.Fatal("model submit: ", err)
		}
		nativeWrite(t, filepath.Join(dest, id+"-model-pending.json"), pending)
		review, err := prepared.Review(t.Context(), e, pending.ProposalID)
		if err != nil {
			t.Fatal("model review: ", err)
		}
		nativeWrite(t, filepath.Join(dest, id+"-model-review.json"), review)
		approval := Approval{RequestID: req + "/admit", Subject: review.Subject, DisplaySHA256: review.DisplaySHA256, PackageSHA256: review.PackageSHA256, Reason: "TEST APPROVAL STUB: original model revalidated with exact lossless source wrappers; fixed disposable public fixture only."}
		published, err := prepared.Admit(t.Context(), e, q, review, approval)
		if err != nil {
			t.Fatal("model admit: ", err)
		}
		nativeWrite(t, filepath.Join(dest, id+"-model-publication.json"), map[string]any{"original_receipt": input.Receipts[n], "original_package_sha256": validation.Digest(raw), "native_package_sha256": validation.Digest(rebound), "approval": approval, "publication": published})
		modelRecord, err := ReadRecord(t.Context(), q, published.CanonicalID)
		if err != nil {
			t.Fatal(err)
		}
		path := input.Report.Paths[n]
		statementObj := map[string]any{"schema_version": "pouch-recorded-repair-binding/v1", "path_id": id, "conclusion_kind": "recorded_file_replay_binding", "observed_fact": false, "new_execution": false, "input_run": repairRun, "original_authority_sha256": repairAuthority, "original_bundle_sha256": repairBundle, "execution_report_sha256": repairReport, "original_manifest_sha256": repairManifest, "original_package_sha256": path.PackageSHA256, "native_authority_sha256": authorityHash, "native_package_sha256": validation.Digest(rebound), "native_model_result": modelRecord.CanonicalID, "native_execution_record": reportSource.CanonicalID, "source_map_sha256": validation.Digest(repairJSON(t, sourceMap)), "original_source_binding": original.SourceBinding, "initial": path.Initial, "patched": path.Patched, "final": path.Final, "events_checked": len(path.Events), "forward_actions": pkg.Forward.Actions, "return_actions": pkg.Return.Actions, "recorded_restoration": true, "scope": "Exact recorded three-file hashes/modes and events match validated traces. No new patch execution or official test; historical SWE status remains FAIL / CONTROL_REPRODUCTION_MISMATCH. No HTTP/DB/external effects or general rollback guarantee."}
		statement, err := json.Marshal(statementObj)
		if err != nil {
			t.Fatal(err)
		}
		bound := admitRepairBinding(t, cfg, i, e, q, id+"-execution", string(statement), []Record{modelRecord, reportSource})
		rows = append(rows, map[string]any{"path_id": id, "original_package_sha256": path.PackageSHA256, "native_package_sha256": validation.Digest(rebound), "model_canonical_id": modelRecord.CanonicalID, "execution_binding_canonical_id": bound.CanonicalID, "model_statement_sha256": modelRecord.StatementSHA256, "execution_statement_sha256": bound.StatementSHA256, "events_checked": 6, "exact_final_hashes_and_modes": true})
	}
	// A distinct final query pass keeps publication-time and final readbacks separate.
	for _, row := range rows {
		for _, kind := range []string{"model", "execution"} {
			idKey := kind + "_canonical_id"
			if kind == "execution" {
				idKey = "execution_binding_canonical_id"
			}
			record, err := ReadRecord(t.Context(), q, row[idKey].(string))
			if err != nil || record.StatementSHA256 != row[kind+"_statement_sha256"] {
				t.Fatal("final exact readback failed: ", err)
			}
			nativeWrite(t, filepath.Join(dest, row["path_id"].(string)+"-"+kind+"-final-readback.json"), record)
		}
	}
	// Recheck every native source wrapper by decoding it back to the exact source;
	// record-only IDs and source metadata are never mistaken for original hashes.
	for _, row := range sourceMap {
		record, err := ReadRecord(t.Context(), q, row["canonical_id"])
		if err != nil {
			t.Fatal(err)
		}
		raw, err := decodeRepairArtifact([]byte(record.Statement), row["original_id"], row["original_sha256"])
		if err != nil || !bytes.Equal(raw, input.Inputs.Content[row["original_id"]]) {
			t.Fatal("final source content mismatch: ", err)
		}
	}
	reportBack, err := ReadRecord(t.Context(), q, reportSource.CanonicalID)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRepairArtifact([]byte(reportBack.Statement), repairRun+"/replay/result.json", repairReport)
	if err != nil || !bytes.Equal(decoded, input.ReportRaw) {
		t.Fatal("final report bytes mismatch: ", err)
	}
	nativeWrite(t, filepath.Join(dest, "summary.json"), map[string]any{"schema_version": "pouch-repair-native-acceptance/v1", "run_id": cfg.RunID, "status": "PASS", "finished_at": time.Now().UTC().Format(time.RFC3339Nano), "original_sources": 8, "execution_report_sources": 1, "source_readbacks_exact_decoded_bytes": 9, "original_packages_revalidated": 6, "rebound_packages_revalidated": 6, "model_derived_admissions": 6, "execution_binding_derived_admissions": 6, "final_fresh_result_readbacks": 12, "recorded_events_checked": 36, "new_sat_queries": 0, "new_patch_operations": 0, "new_official_tests": 0, "approval": "TEST APPROVAL STUB; fresh isolated cluster only", "paths": rows})
}
