package ahe

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// ErrContractMismatch reports that the contract AHE publishes is not the one
// the operator pinned, or names a version Pouch cannot parse. It is distinct
// from GOVERNANCE_UNAVAILABLE, which means a check could not be completed.
var ErrContractMismatch = errors.New("CONTRACT_MISMATCH")

const queryContractVersion = "ahe-query-contract/v1"

// queryContract is the part of get_query_contract that Pouch relies on. The
// remaining fields are covered by the operator's pin of the exact bytes.
type queryContract struct {
	Contract           string   `json:"contract"`
	ProjectionVersions []string `json:"projection_versions"`
	UsePolicies        []string `json:"use_policies"`
}

// CheckContract reads get_query_contract through a Query profile and requires
// its exact response bytes to match the operator's pin. It then requires every
// advertised projection version to be one Pouch parses, and both use policies
// to be offered. It describes the AHE binary, not the attached database, and
// says nothing about semantic preconditions such as lineage registration.
func CheckContract(ctx context.Context, query *Profile, wantSHA256 string) error {
	if query == nil || query.kind != ProfileQuery {
		return ErrCapabilityUnavailable
	}
	if !validDigest(wantSHA256) {
		return errors.New("contract_sha256 must be the SHA-256 of the pinned get_query_contract response")
	}
	raw, err := query.call(ctx, "get_query_contract", map[string]any{})
	if err != nil {
		return err
	}
	return checkContractBytes(raw, wantSHA256)
}

func checkContractBytes(raw []byte, wantSHA256 string) error {
	if got := validation.Digest(raw); got != wantSHA256 {
		return fmt.Errorf("%w: pinned contract %s, AHE returned %s", ErrContractMismatch, wantSHA256, got)
	}
	var c queryContract
	if err := decodeGovernance(raw, &c, false); err != nil {
		return fmt.Errorf("%w: unreadable contract: %v", ErrContractMismatch, err)
	}
	if c.Contract != queryContractVersion {
		return fmt.Errorf("%w: unsupported contract %q", ErrContractMismatch, c.Contract)
	}
	if len(c.ProjectionVersions) == 0 {
		return fmt.Errorf("%w: no projection version advertised", ErrContractMismatch)
	}
	for _, v := range c.ProjectionVersions {
		if !validProjection(v) {
			return fmt.Errorf("%w: unsupported projection %q", ErrContractMismatch, v)
		}
	}
	for _, policy := range []string{CurrentActionPolicy, GroundedActionPolicy} {
		if !slices.Contains(c.UsePolicies, policy) {
			return fmt.Errorf("%w: policy %q not offered", ErrContractMismatch, policy)
		}
	}
	return nil
}
