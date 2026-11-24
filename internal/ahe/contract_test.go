package ahe

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// The samples in testdata/ahe-golden are byte-for-byte copies of AHE's own
// golden outputs (ahe-mcp 3f0ede7, branch feat/public-contract-versioning,
// internal/evidencequerymcp/testdata/golden). They are protocol samples of a
// synthetic blocked root, not evidence about any database.
func goldenSample(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "ahe-golden", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestGoldenContractAccepted(t *testing.T) {
	raw := goldenSample(t, "get_query_contract.json")
	if err := checkContractBytes(raw, validation.Digest(raw)); err != nil {
		t.Fatal(err)
	}
}

func TestContractMismatch(t *testing.T) {
	golden := string(goldenSample(t, "get_query_contract.json"))
	for name, tc := range map[string]struct{ from, to string }{
		"unknown projection": {`"evidence-use-projection/v2"`, `"evidence-use-projection/v2","evidence-use-projection/v3"`},
		"no projection":      {`["evidence-use-projection/v2"]`, `[]`},
		"other contract":     {`ahe-query-contract/v1`, `ahe-query-contract/v2`},
		"missing policy":     {`,"grounded-current-action/v1"`, ``},
	} {
		t.Run(name, func(t *testing.T) {
			changed := strings.Replace(golden, tc.from, tc.to, 1)
			if changed == golden {
				t.Fatal("test edit did not apply")
			}
			err := checkContractBytes([]byte(changed), validation.Digest([]byte(changed)))
			if !errors.Is(err, ErrContractMismatch) || errors.Is(err, ErrGovernanceUnavailable) {
				t.Fatal(err)
			}
		})
	}
	t.Run("pin differs", func(t *testing.T) {
		err := checkContractBytes([]byte(golden), validation.Digest([]byte("another")))
		if !errors.Is(err, ErrContractMismatch) || !strings.Contains(err.Error(), validation.Digest([]byte(golden))) {
			t.Fatal(err)
		}
	})
}

func TestCheckContractNeedsQueryProfileAndPin(t *testing.T) {
	if err := CheckContract(t.Context(), nil, strings.Repeat("a", 64)); !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatal(err)
	}
	c, calls := testClient(t, "")
	q := testProfile(t, c, ProfileQuery, "get_evidence_record")
	if err := CheckContract(t.Context(), q, ""); err == nil || errors.Is(err, ErrContractMismatch) {
		t.Fatal("missing pin must be a configuration error, not a mismatch:", err)
	}
	if raw, _ := os.ReadFile(calls); len(raw) != 0 {
		t.Fatal("contract was read without a valid pin")
	}
}

// Pouch parses the current AHE responses unchanged; blocked and unaccepted
// samples stay ineligible rather than turning into an error.
func TestGoldenGovernanceResponses(t *testing.T) {
	roots := []string{"canon-node:a"}
	var use struct {
		Assessment useAssessment `json:"assessment"`
	}
	if err := decodeGovernance(goldenSample(t, "get_evidence_use_state.json"), &use, false); err != nil {
		t.Fatal(err)
	}
	if ok, err := use.Assessment.eligible(CurrentActionPolicy, roots); err != nil || ok {
		t.Fatal(ok, err)
	}
	var plan struct {
		Assessment useAssessment `json:"assessment"`
		Actions    []ActionBasis `json:"actions"`
		Accepted   *bool         `json:"accepted"`
	}
	if err := decodeGovernance(goldenSample(t, "check_current_evidence_plan.json"), &plan, false); err != nil {
		t.Fatal(err)
	}
	if ok, err := plan.Assessment.eligible(CurrentActionPolicy, roots); err != nil || ok {
		t.Fatal(ok, err)
	}
	if plan.Accepted == nil || *plan.Accepted || len(plan.Actions) != 1 || plan.Actions[0].ActionID != "act" || !slices.Equal(plan.Actions[0].BasisIDs, roots) {
		t.Fatal(plan)
	}
}
