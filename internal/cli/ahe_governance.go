package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/Yui-Qi-Tang/pouch/internal/ahe"
	"github.com/Yui-Qi-Tang/pouch/internal/planning"
	"github.com/Yui-Qi-Tang/pouch/internal/run"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func readBasis(authority []byte, hash, request, path, basisHash string) (*ahe.Basis, error) {
	if path == "" || basisHash == "" {
		return nil, errors.New("action-basis and action-basis-sha256 required")
	}
	raw, err := read(path, validation.MaxBytes)
	if err != nil {
		return nil, err
	}
	return ahe.NewBasis(authority, hash, request, raw, basisHash)
}

func governanceFailure(out io.Writer, check ahe.GovernanceCheck, err error) error {
	if e := output(out, struct {
		Status     string              `json:"status"`
		Governance ahe.GovernanceCheck `json:"governance"`
	}{check.Status, check}); e != nil {
		return e
	}
	return err
}

type governanceReport struct {
	SchemaVersion   string               `json:"schema_version"`
	AuthoritySHA256 string               `json:"authority_sha256"`
	RequestID       string               `json:"request_id"`
	BasisSHA256     string               `json:"basis_sha256"`
	Before          ahe.GovernanceCheck  `json:"before"`
	After           *ahe.GovernanceCheck `json:"after,omitempty"`
	// Digest of json.Marshal(bundle.Search), not a claim about raw bundle bytes.
	SearchSHA256  string            `json:"search_json_sha256,omitempty"`
	PackageSHA256 map[string]string `json:"package_sha256,omitempty"`
}

func emittedActions(result planning.Result) []string {
	ids := []string{}
	for _, path := range result.Forward {
		ids = append(ids, path.Trace.Actions...)
	}
	for _, ret := range result.Returns {
		if ret.Trace != nil {
			ids = append(ids, ret.Trace.Actions...)
		}
	}
	return ids
}

func (r *governanceReport) bindResult(bundle run.Bundle) error {
	raw, err := json.Marshal(bundle.Search)
	if err != nil {
		return err
	}
	r.SearchSHA256 = validation.Digest(raw)
	r.PackageSHA256 = map[string]string{}
	for _, path := range bundle.Paths {
		if path.Receipt != nil {
			r.PackageSHA256[path.PackageFile] = path.Receipt.PackageSHA256
		}
	}
	return nil
}

func saveGovernanceFile(dir, name string, raw []byte) error {
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func saveGovernanceReport(dir string, r governanceReport) error {
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return saveGovernanceFile(dir, "ahe-governance.json", append(raw, '\n'))
}
