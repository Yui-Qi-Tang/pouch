// Package candidate compiles caller-declared finite options into a Pouch model.
// It does not infer requirements from source text or invent program edits.
package candidate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strconv"
	"strings"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

const Version = "pouch-candidate-space/v1"
const bindingKey = "candidate_space_sha256"

var name = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// Option.Text maps file paths to literal template substitutions. A choice may
// affect multiple files. Text is inert: templates never execute code.
type Option struct {
	ID   string            `json:"id"`
	Text map[string]string `json:"text"`
}
type Group struct {
	ID      string   `json:"id"`
	Options []Option `json:"options"`
}

// Constraint is a forbidden partial choice assignment, supplied by a semantic
// producer. Evidence IDs identify its basis, not a proof of source entailment.
type Constraint struct {
	ID       string           `json:"id"`
	When     validation.State `json:"when"`
	Evidence []string         `json:"evidence"`
}
type File struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Template string `json:"template"`
}
type Space struct {
	SchemaVersion string            `json:"schema_version"`
	RequestID     string            `json:"request_id"`
	SourceBinding map[string]string `json:"source_binding"`
	Assumptions   map[string]string `json:"assumptions"`
	Groups        []Group           `json:"groups"`
	Constraints   []Constraint      `json:"constraints"`
	Files         []File            `json:"files"`
}

// Prepared owns its decoded inputs. Authority bytes are deterministic for this
// compiler version and bound to the exact, externally pinned space bytes.
type Prepared struct {
	space     Space
	digest    string
	authority []byte
	validator *validation.Authority
}

func Prepare(raw []byte, expected string) (*Prepared, error) {
	if validation.Digest(raw) != expected {
		return nil, errors.New("candidate space digest mismatch")
	}
	var s Space
	if err := decode(raw, &s); err != nil {
		return nil, err
	}
	if s.SchemaVersion != Version || s.Assumptions == nil || len(s.Groups) == 0 || len(s.Groups) > 8 || len(s.Files) == 0 || len(s.Files) > 32 || len(s.Constraints) > 256 {
		return nil, errors.New("candidate space bounds or schema")
	}
	if _, ok := s.Assumptions[bindingKey]; ok {
		return nil, errors.New("reserved assumption key")
	}
	paths := map[string]bool{}
	foldedPaths := map[string]bool{}
	for _, f := range s.Files {
		if !safePath(f.Path) || foldedPaths[strings.ToLower(f.Path)] || !validHash(f.SHA256) {
			return nil, errors.New("invalid candidate file identity")
		}
		paths[f.Path] = true
		foldedPaths[strings.ToLower(f.Path)] = true
	}
	c := validation.Contract{SchemaVersion: validation.Version, RequestID: s.RequestID, SourceBinding: s.SourceBinding, Assumptions: maps.Clone(s.Assumptions), Model: validation.Model{Fields: map[string][]string{}, Actions: []validation.Action{}, Forbidden: []validation.State{}}, Start: validation.State{}, Goal: validation.State{"phase": "done"}, Baseline: validation.State{}, ForwardLimit: len(s.Groups) + 1, ReturnLimit: len(s.Groups) + 1}
	c.Assumptions[bindingKey] = expected
	domains := map[string]map[string]bool{}
	phases := []string{}
	for i := 0; i <= len(s.Groups); i++ {
		phases = append(phases, strconv.Itoa(i))
	}
	phases = append(phases, "done")
	c.Model.Fields["phase"] = phases
	c.Start["phase"] = "0"
	c.Baseline["phase"] = "0"
	fields := []string{"phase"}
	for _, g := range s.Groups {
		if !name.MatchString(g.ID) || g.ID == "phase" || domains[g.ID] != nil || len(g.Options) == 0 || len(g.Options) > 16 {
			return nil, errors.New("invalid choice group")
		}
		domains[g.ID] = map[string]bool{}
		c.Model.Fields[g.ID] = []string{"unset"}
		fields = append(fields, g.ID)
		c.Start[g.ID] = "unset"
		c.Baseline[g.ID] = "unset"
		for _, o := range g.Options {
			if !name.MatchString(o.ID) || o.ID == "unset" || domains[g.ID][o.ID] || o.Text == nil {
				return nil, errors.New("invalid option")
			}
			domains[g.ID][o.ID] = true
			c.Model.Fields[g.ID] = append(c.Model.Fields[g.ID], o.ID)
			for path, text := range o.Text {
				if !paths[path] || strings.Contains(text, "{{pouch:") {
					return nil, errors.New("invalid option substitution")
				}
			}
		}
	}
	add := func(id string, guard, effect validation.State) {
		frame := []string{}
		for _, f := range fields {
			if _, ok := effect[f]; !ok {
				frame = append(frame, f)
			}
		}
		c.Model.Actions = append(c.Model.Actions, validation.Action{ID: id, Guard: guard, Effect: effect, Frame: frame})
	}
	for i, g := range s.Groups {
		for _, o := range g.Options {
			add("choose_"+g.ID+"_"+o.ID, validation.State{"phase": strconv.Itoa(i), g.ID: "unset"}, validation.State{"phase": strconv.Itoa(i + 1), g.ID: o.ID})
			add("withdraw_"+g.ID+"_"+o.ID, validation.State{"phase": strconv.Itoa(i + 1), g.ID: o.ID}, validation.State{"phase": strconv.Itoa(i), g.ID: "unset"})
		}
	}
	end := strconv.Itoa(len(s.Groups))
	add("submit_candidate", validation.State{"phase": end}, validation.State{"phase": "done"})
	add("clear_submission", validation.State{"phase": "done"}, validation.State{"phase": end})
	seen := map[string]bool{}
	for _, k := range s.Constraints {
		if !name.MatchString(k.ID) || seen[k.ID] || len(k.When) == 0 || len(k.Evidence) == 0 {
			return nil, errors.New("invalid constraint")
		}
		seen[k.ID] = true
		cube := validation.State{"phase": "done"}
		for f, v := range k.When {
			if !domains[f][v] {
				return nil, errors.New("constraint outside choice domain")
			}
			cube[f] = v
		}
		for _, id := range k.Evidence {
			if _, ok := s.SourceBinding[id]; !ok {
				return nil, errors.New("constraint evidence missing")
			}
		}
		c.Model.Forbidden = append(c.Model.Forbidden, cube)
	}
	// Validate every token before search, including substitutions not selected yet.
	for _, f := range s.Files {
		remainder := f.Template
		for _, g := range s.Groups {
			token := "{{pouch:" + g.ID + "}}"
			used := strings.Contains(remainder, token)
			for _, o := range g.Options {
				_, present := o.Text[f.Path]
				if present != used {
					return nil, fmt.Errorf("template/option mismatch for %s/%s", f.Path, g.ID)
				}
			}
			remainder = strings.ReplaceAll(remainder, token, "")
		}
		if strings.Contains(remainder, "{{pouch:") {
			return nil, errors.New("unknown template token")
		}
	}
	authority, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	authority = append(authority, '\n')
	a, err := validation.NewAuthority(authority, validation.Digest(authority))
	if err != nil {
		return nil, err
	}
	return &Prepared{s, expected, authority, a}, nil
}
func (p *Prepared) Authority() []byte { return append([]byte(nil), p.authority...) }

// Validate replays the full certificate before exposing the selected choices.
func (p *Prepared) Validate(ctx context.Context, raw []byte) (validation.Receipt, validation.State, error) {
	r, err := p.validator.Validate(ctx, p.space.RequestID, raw)
	if err != nil {
		return r, nil, err
	}
	var pkg validation.Package
	if err = json.Unmarshal(raw, &pkg); err != nil {
		return r, nil, err
	}
	choices := maps.Clone(pkg.Forward.States[len(pkg.Forward.States)-1])
	delete(choices, "phase")
	return r, choices, nil
}
