// Package automata checks caller-owned synchronous Boolean automata. Rule
// tables live in the authority bytes, so its digest fixes the rule and replay
// needs neither the encoder nor the program that produced the tables. The
// package-local README.md defines the format and its limits.
package automata

import (
	"encoding/hex"
	"slices"

	"github.com/Yui-Qi-Tang/calx/automaton"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// Version identifies the authority schema.
const Version = "pouch-automaton/v1"

const (
	maxCells          = 1 << 20
	maxBits           = 16
	maxFanIn          = 12
	maxRules          = 256
	maxSteps          = 4095
	maxSourceBindings = 32
)

// Rule is the truth table of one cell kind. Table holds the output bits of every
// input row in row order; see README.md for the bit layout.
type Rule struct {
	Inputs  int    `json:"inputs"`
	Choices int    `json:"choices"`
	Table   string `json:"table"`
}

// Cell selects a rule and wires its inputs. Each input is a state bit address,
// cell*bits+index.
type Cell struct {
	Rule   int   `json:"rule"`
	Inputs []int `json:"inputs"`
}

// Model declares a synchronous automaton. It is not extracted source meaning.
type Model struct {
	Bits  int    `json:"bits"`
	Rules []Rule `json:"rules"`
	Cells []Cell `json:"cells"`
}

// Pattern constrains the state bits set in Mask to their values in Value.
type Pattern struct {
	Mask  string `json:"mask"`
	Value string `json:"value"`
}

// Contract is supplied out of band by the caller, never selected by the sender.
type Contract struct {
	SchemaVersion string            `json:"schema_version"`
	RequestID     string            `json:"request_id"`
	SourceBinding map[string]string `json:"source_binding"`
	Assumptions   map[string]string `json:"assumptions"`
	Model         Model             `json:"model"`
	Start         string            `json:"start"`
	Goal          Pattern           `json:"goal"`
	Baseline      string            `json:"baseline"`
	ForwardLimit  int               `json:"forward_limit"`
	ReturnLimit   int               `json:"return_limit"`
}

// Authority owns a private decoded copy and is immutable after construction.
type Authority struct {
	system *system
	start  automaton.State
}

// NewAuthority verifies the external digest and rejects unsupported models.
func NewAuthority(raw []byte, expected string) (*Authority, error) {
	if !validDigest(expected) || validation.Digest(raw) != expected {
		return nil, reject("AUTHORITY_HASH_MISMATCH")
	}
	var c Contract
	if err := validation.Decode(raw, &c); err != nil {
		return nil, err
	}
	if c.SchemaVersion != Version || c.RequestID == "" || len(c.RequestID) > 200 || len(c.SourceBinding) == 0 || len(c.SourceBinding) > maxSourceBindings {
		return nil, reject("AUTHORITY_CONTRACT")
	}
	for id, digest := range c.SourceBinding {
		if id == "" || len(id) > 200 || !validDigest(digest) {
			return nil, reject("SOURCE_BINDING")
		}
	}
	if c.ForwardLimit < 0 || c.ForwardLimit > maxSteps || c.ReturnLimit < 0 || c.ReturnLimit > maxSteps {
		return nil, reject("MODEL_BOUND")
	}
	s, err := newSystem(c.Model)
	if err != nil {
		return nil, err
	}
	stateBits := len(s.cells) * s.bits
	start, ok := decodeBits(c.Start, stateBits)
	if !ok {
		return nil, reject("STATE_ENCODING")
	}
	if _, ok := decodeBits(c.Baseline, stateBits); !ok {
		return nil, reject("STATE_ENCODING")
	}
	mask, maskOK := decodeBits(c.Goal.Mask, stateBits)
	value, valueOK := decodeBits(c.Goal.Value, stateBits)
	if !maskOK || !valueOK || !slices.Contains(mask, true) {
		return nil, reject("GOAL_PATTERN")
	}
	for i := range value {
		if value[i] && !mask[i] {
			return nil, reject("GOAL_PATTERN")
		}
	}
	return &Authority{system: s, start: start}, nil
}

// System returns the automaton the authority declares.
func (a *Authority) System() automaton.System { return a.system }

// Start returns an independent copy of the start state.
func (a *Authority) Start() automaton.State { return slices.Clone(a.start) }

func reject(code string) error { return &validation.Rejection{Code: code} }

func validDigest(s string) bool {
	raw, err := hex.DecodeString(s)
	return err == nil && len(raw) == 32 && s == hex.EncodeToString(raw)
}

// decodeBits reads an n-bit string written by encodeBits. Uppercase digits, a
// wrong length and set padding bits are rejected, so each state has one spelling.
func decodeBits(text string, n int) ([]bool, bool) {
	raw, err := hex.DecodeString(text)
	if err != nil || len(raw) != (n+7)/8 || text != hex.EncodeToString(raw) {
		return nil, false
	}
	bits := make([]bool, n)
	for i := range bits {
		bits[i] = raw[i/8]>>(i%8)&1 == 1
	}
	if n%8 != 0 && raw[len(raw)-1]>>(n%8) != 0 {
		return nil, false
	}
	return bits, true
}

func encodeBits(bits []bool) string {
	raw := make([]byte, (len(bits)+7)/8)
	for i, set := range bits {
		if set {
			raw[i/8] |= 1 << (i % 8)
		}
	}
	return hex.EncodeToString(raw)
}
