package ahe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Yui-Qi-Tang/pouch/internal/projection"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// These are real MCP integration stages, opt-in only. Bootstrap creates a fresh
// disposable PostgreSQL cluster; ordinary go test never touches a native store.
type nativeConfig struct {
	Schema   string                            `json:"schema_version"`
	RunID    string                            `json:"run_id"`
	Root     string                            `json:"work_dir"`
	Project  string                            `json:"pouch_project"`
	Commands map[ProfileKind]Command           `json:"commands"`
	Pins     map[ProfileKind]map[string]string `json:"pins"`
}

func nativeFixture(t *testing.T) nativeConfig {
	t.Helper()
	path := os.Getenv("POUCH_AHE_NATIVE_CONFIG")
	if path == "" {
		t.Skip("fresh isolated native MCP fixture not configured")
	}
	var cfg nativeConfig
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &cfg) != nil || cfg.Schema != "pouch-disposable-native/v1" || !filepath.IsAbs(cfg.Root) || !strings.HasPrefix(cfg.RunID, "pouch-native-") {
		t.Fatal("invalid isolated native fixture")
	}
	if os.Getenv("POUCH_AHE_TEST_APPROVAL") != "isolated-synthetic-public-fixtures-only" {
		t.Fatal("explicit isolated TEST APPROVAL STUB authorization missing")
	}
	return cfg
}
func nativeWrite(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
func nativeClient(t *testing.T, cfg nativeConfig, kind ProfileKind) *Client {
	t.Helper()
	c, err := Start(t.Context(), cfg.Commands[kind])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}
func nativeProfile(t *testing.T, cfg nativeConfig, kind ProfileKind) *Profile {
	t.Helper()
	p, err := nativeClient(t, cfg, kind).Bind(kind, cfg.Pins[kind])
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var nativeTraceSequence atomic.Uint64

func nativeCall(t *testing.T, p *Profile, name string, args any) json.RawMessage {
	t.Helper()
	raw, err := p.call(t.Context(), name, args)
	if err != nil {
		if e, ok := err.(*ToolError); ok {
			t.Fatalf("native tool %s refused: %s", name, e.Payload)
		}
		t.Fatal(err)
	}
	nativeWrite(t, filepath.Join(filepath.Dir(os.Getenv("POUCH_AHE_NATIVE_CONFIG")), fmt.Sprintf("trace-%s-%06d.json", strings.Split(t.Name(), "/")[0], nativeTraceSequence.Add(1))), map[string]any{"tool": name, "arguments": args, "response": raw})
	return raw
}

// Discovery freezes raw advertised schemas before source writes or fault tests.
// The caller additionally records the exact AHE build revision and binary hashes.
func TestNativeDiscovery(t *testing.T) {
	cfg := nativeFixture(t)
	cfg.Pins = map[ProfileKind]map[string]string{}
	for _, kind := range []ProfileKind{ProfileQuery, ProfileIntake, ProfileSourceClaimReviewer, ProfileEndpointReviewer} {
		c := nativeClient(t, cfg, kind)
		cfg.Pins[kind] = map[string]string{}
		for _, tool := range c.Tools() {
			if _, ok := allowed[kind][tool.Name]; ok {
				cfg.Pins[kind][tool.Name] = tool.SchemaSHA256()
			}
		}
		if len(cfg.Pins[kind]) != len(allowed[kind]) {
			t.Fatalf("%s missing required public MCP tools", kind)
		}
		nativeWrite(t, filepath.Join(cfg.Root, string(kind)+"-discovery.json"), c.Tools())
	}
	nativeWrite(t, os.Getenv("POUCH_AHE_NATIVE_CONFIG"), cfg)
}
func TestNativeSources(t *testing.T) {
	cfg := nativeFixture(t)
	q := nativeProfile(t, cfg, ProfileQuery)
	i := nativeProfile(t, cfg, ProfileIntake)
	reviewer := nativeProfile(t, cfg, ProfileSourceClaimReviewer)
	total := 0
	for _, name := range []string{"synthetic-key-rotation", "django-13344"} {
		t.Run(name, func(t *testing.T) {
			base := filepath.Join(cfg.Project, "testdata", name)
			dest := filepath.Join(cfg.Root, name)
			if err := os.Mkdir(dest, 0700); err != nil {
				t.Fatal("refusing repeated native source stage: ", err)
			}
			if err := os.Mkdir(filepath.Join(dest, "sources"), 0700); err != nil {
				t.Fatal(err)
			}
			authorityRaw, err := os.ReadFile(filepath.Join(base, "authority.json"))
			if err != nil {
				t.Fatal(err)
			}
			original, err := validation.NewAuthority(authorityRaw, validation.Digest(authorityRaw))
			if err != nil {
				t.Fatal(err)
			}
			c := original.Contract()
			c.RequestID = cfg.RunID + "/" + name
			c.SourceBinding = map[string]string{}
			var sourceList struct {
				Sources []struct{ ID, File, SHA256 string }
			}
			raw, err := os.ReadFile(filepath.Join(base, "sources.json"))
			if err != nil || json.Unmarshal(raw, &sourceList) != nil {
				t.Fatal("fixture sources manifest invalid")
			}
			var records []Record
			var sourceEntries []map[string]string
			var sourceLog []any
			for n, source := range sourceList.Sources {
				content, err := os.ReadFile(filepath.Join(base, source.File))
				if err != nil || validation.Digest(content) != source.SHA256 {
					t.Fatal("fixture source bytes mismatch")
				}
				request := cfg.RunID + "/" + name + fmt.Sprintf("/source-%02d", n)
				submitted := nativeCall(t, i, "submit_external_source", map[string]any{"schema_version": "external-source-envelope-v1", "request_id": request, "source_system": "pouch-native-test", "source_namespace": cfg.RunID, "object_type": "synthetic_or_public_fixture", "object_id": name + fmt.Sprint(n), "revision": "v1", "source_location": "urn:pouch:fixture:" + name + fmt.Sprint(n), "title": "TEST FIXTURE: " + name, "content_format": "text/plain", "content_fidelity": "verbatim", "content": string(content), "coverage": "full_document", "limitations": []string{}, "collector_id": "pouch-native-test", "connector_id": "pouch-native-test", "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
				var intake struct {
					SourceSnapshotID string `json:"source_snapshot_id"`
					ViewID           string `json:"extraction_view_id"`
					Hash             string `json:"raw_content_hash"`
					Spans            []struct {
						ID string `json:"span_id"`
					} `json:"spans"`
				}
				if json.Unmarshal(submitted, &intake) != nil || intake.Hash != "sha256:"+source.SHA256 || len(intake.Spans) == 0 {
					t.Fatal("native source receipt mismatch")
				}
				refs := []string{}
				for _, span := range intake.Spans {
					refs = append(refs, span.ID)
				}
				proposed := nativeCall(t, i, "submit_extractor_output", map[string]any{"request_id": request + "/proposal", "source_snapshot_id": intake.SourceSnapshotID, "extraction_view_id": intake.ViewID, "extractor_definition": map[string]string{"name": "pouch-native-exact-fixture", "version": "v1"}, "extractor_output": map[string]any{"proposals": []any{map[string]any{"proposal_local_id": "fixture", "statement_text": string(content), "evidence_refs": refs}}}})
				var proposal struct {
					Attempt string `json:"extraction_attempt_id"`
					ID      string `json:"proposal_occurrence_id"`
					Count   int    `json:"proposal_count"`
					Status  string `json:"status"`
				}
				if json.Unmarshal(proposed, &proposal) != nil || proposal.Count != 1 || proposal.Status != "pending" {
					t.Fatal("native proposal mismatch")
				}
				reviewed := nativeCall(t, reviewer, "get_source_claim_review", map[string]string{"extraction_attempt_id": proposal.Attempt, "proposal_occurrence_id": proposal.ID})
				var review struct {
					Subject json.RawMessage `json:"subject"`
				}
				if json.Unmarshal(reviewed, &review) != nil || len(review.Subject) == 0 {
					t.Fatal("review subject missing")
				}
				admitted := nativeCall(t, reviewer, "admit_reviewed_source_claim", map[string]any{"extraction_attempt_id": proposal.Attempt, "expected_subject": review.Subject, "decision": "approved", "decision_reason": "TEST APPROVAL STUB: exact frozen synthetic/public fixture in this disposable cluster only; not a human or semantic certification."})
				var admission struct {
					ID string `json:"canonical_ref"`
				}
				if json.Unmarshal(admitted, &admission) != nil || admission.ID == "" {
					t.Fatal("native admission ID missing")
				}
				record, err := ReadRecord(t.Context(), q, admission.ID)
				if err != nil || record.StatementSHA256 != source.SHA256 {
					t.Fatal("fresh source readback mismatch: ", err)
				}
				c.SourceBinding[record.CanonicalID] = record.StatementSHA256
				for key, value := range c.Assumptions {
					c.Assumptions[key] = strings.ReplaceAll(value, source.ID, record.CanonicalID)
				}
				target := fmt.Sprintf("sources/source-%02d.txt", n)
				if err := os.WriteFile(filepath.Join(dest, target), content, 0600); err != nil {
					t.Fatal(err)
				}
				sourceEntries = append(sourceEntries, map[string]string{"id": record.CanonicalID, "file": target, "sha256": record.StatementSHA256})
				records = append(records, record)
				sourceLog = append(sourceLog, map[string]any{"intake": submitted, "proposal": proposed, "review": reviewed, "admission": admitted})
				total++
			}
			fresh, _ := json.MarshalIndent(c, "", "  ")
			fresh = append(fresh, '\n')
			if err := os.WriteFile(filepath.Join(dest, "authority.json"), fresh, 0600); err != nil {
				t.Fatal(err)
			}
			nativeWrite(t, filepath.Join(dest, "sources.json"), map[string]any{"schema_version": "pouch-sources/v1", "authority_sha256": validation.Digest(fresh), "request_id": c.RequestID, "sources": sourceEntries})
			nativeWrite(t, filepath.Join(dest, "source-readbacks.json"), records)
			nativeWrite(t, filepath.Join(dest, "source-native-ledger.json"), sourceLog)
			ids := []string{}
			for _, record := range records {
				ids = append(ids, record.CanonicalID)
			}
			slices.Sort(ids)
			snapshot, err := ReadSnapshot(t.Context(), q, ReadRequest{RootNodeIDs: ids, Relations: []string{}, MaxDepth: 2, MaxNodes: 128, MaxEdges: 512})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := projection.Decode(snapshot.Artifact, snapshot.ArtifactSHA256); err != nil {
				t.Fatal("native artifact projection: ", err)
			}
			if err := os.WriteFile(filepath.Join(dest, "artifact.json"), snapshot.Artifact, 0600); err != nil {
				t.Fatal(err)
			}
			nativeWrite(t, filepath.Join(dest, "snapshot.json"), snapshot)
			nativeWrite(t, filepath.Join(dest, "input-binding.json"), map[string]any{"original_authority_sha256": validation.Digest(authorityRaw), "new_authority_sha256": validation.Digest(fresh), "new_request_id": c.RequestID, "model_unchanged": true, "source_count": len(records)})
		})
	}
	if total != 9 {
		t.Fatalf("expected 9 fresh source admissions/readbacks, got %d", total)
	}
	nativeWrite(t, filepath.Join(cfg.Root, "sources-summary.json"), map[string]any{"status": "PASS", "sources": total, "contexts": 2, "scope": "native public MCP source admission/readback and fixed model identity rebinding only; search/publication pending"})
}

func nativeWriteExclusive(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(raw)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// TestNativeSnapshots reads the already admitted fixture sources without writes.
func TestNativeSnapshots(t *testing.T) {
	cfg := nativeFixture(t)
	q := nativeProfile(t, cfg, ProfileQuery)
	for _, name := range []string{"synthetic-key-rotation", "django-13344"} {
		t.Run(name, func(t *testing.T) {
			dest := filepath.Join(cfg.Root, name)
			raw, err := os.ReadFile(filepath.Join(dest, "authority.json"))
			if err != nil {
				t.Fatal(err)
			}
			var contract validation.Contract
			if err := json.Unmarshal(raw, &contract); err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for id := range contract.SourceBinding {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			snapshot, err := ReadSnapshot(t.Context(), q, ReadRequest{RootNodeIDs: ids, Relations: []string{}, MaxDepth: 2, MaxNodes: 128, MaxEdges: 512})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := projection.Decode(snapshot.Artifact, snapshot.ArtifactSHA256); err != nil {
				t.Fatal("native artifact projection: ", err)
			}
			if err := nativeWriteExclusive(filepath.Join(dest, "artifact-readback.json"), snapshot.Artifact); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.MarshalIndent(snapshot, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := nativeWriteExclusive(filepath.Join(dest, "snapshot-readback.json"), append(encoded, '\n')); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestNativePublication(t *testing.T) {
	cfg := nativeFixture(t)
	q := nativeProfile(t, cfg, ProfileQuery)
	i := nativeProfile(t, cfg, ProfileIntake)
	e := nativeProfile(t, cfg, ProfileEndpointReviewer)
	total := 0
	returns := 0
	noReturns := 0
	for _, name := range []string{"synthetic-key-rotation", "django-13344"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(cfg.Root, name)
			a, err := os.ReadFile(filepath.Join(root, "authority.json"))
			if err != nil {
				t.Fatal(err)
			}
			var contract validation.Contract
			json.Unmarshal(a, &contract)
			packages, err := filepath.Glob(filepath.Join(root, "result", "packages", "p*.json"))
			if err != nil || len(packages) == 0 {
				t.Fatal("newly generated native-bound search packages missing")
			}
			want := 6
			if name == "django-13344" {
				want = 2
			}
			if len(packages) != want {
				t.Fatalf("expected %d newly generated paths, got %d", want, len(packages))
			}
			dir := filepath.Join(root, "publication")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal("refusing repeated native result stage: ", err)
			}
			for n, path := range packages {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				p, err := Prepare(t.Context(), q, a, validation.Digest(a), contract.RequestID, raw)
				if err != nil {
					t.Fatal(err)
				}
				request := cfg.RunID + "/" + name + fmt.Sprintf("/result-%02d", n)
				pending, err := p.Submit(t.Context(), i, request, time.Now().UTC())
				if err != nil {
					if tool, ok := err.(*ToolError); ok {
						t.Fatalf("submit: %s", tool.Payload)
					}
					t.Fatal(err)
				}
				review, err := p.Review(t.Context(), e, pending.ProposalID)
				if err != nil {
					if tool, ok := err.(*ToolError); ok {
						t.Fatalf("review: %s", tool.Payload)
					}
					t.Fatal(err)
				}
				approval := Approval{RequestID: request + "/admit", Subject: review.Subject, DisplaySHA256: review.DisplaySHA256, PackageSHA256: review.PackageSHA256, Reason: "TEST APPROVAL STUB: exact validated finite-model conclusion, unchanged native display and parent set in this disposable cluster only."}
				publication, err := p.Admit(t.Context(), e, q, review, approval)
				if err != nil {
					if tool, ok := err.(*ToolError); ok {
						t.Fatalf("admit: %s", tool.Payload)
					}
					t.Fatal(err)
				}
				nativeWrite(t, filepath.Join(dir, fmt.Sprintf("p%06d.json", n)), map[string]any{"validation": p.Receipt(), "sources": p.Sources(), "pending": pending, "review": review, "approval": approval, "publication": publication})
				total++
				if p.Receipt().ReturnKind == "return" {
					returns++
				} else {
					noReturns++
				}
			}
		})
	}
	if total != 8 {
		t.Fatalf("expected 8 native result admissions/readbacks, got %d", total)
	}
	nativeWrite(t, filepath.Join(cfg.Root, "publication-summary.json"), map[string]any{"status": "PASS", "public_mcp": true, "cases": 2, "sources": 9, "paths_validated": total, "native_derived_admissions": total, "fresh_result_readbacks": total, "return": returns, "no_return": noReturns, "approval": "TEST APPROVAL STUB in fresh isolated cluster", "scope": "fixed finite models and native storage/readback; not original-problem completeness or official SWE runtime test"})
}
