package cli

import (
	"errors"
	"io"

	"github.com/Yui-Qi-Tang/pouch/internal/automata"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func automatonPrepare(args []string, out, errout io.Writer) error {
	f := flags("automaton-prepare", errout)
	spec := f.String("spec", "", "caller-declared lattice gas spec JSON")
	hash := f.String("sha256", "", "caller-pinned exact spec digest")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *spec == "" || *hash == "" {
		return errors.New("automaton spec and digest required")
	}
	raw, err := read(*spec, validation.MaxBytes)
	if err != nil {
		return err
	}
	authority, err := automata.Prepare(raw, *hash)
	if err != nil {
		return err
	}
	_, err = out.Write(authority)
	return err
}
