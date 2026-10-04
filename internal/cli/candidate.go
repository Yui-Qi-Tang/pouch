package cli

import (
	"context"
	"errors"
	"io"

	"github.com/Yui-Qi-Tang/pouch/internal/candidate"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func candidateCommand(ctx context.Context, command string, args []string, out, errout io.Writer) error {
	f := flags(command, errout)
	evidence := f.String("evidence", "", "caller-pinned runtime record")
	evidenceHash := f.String("evidence-sha256", "", "caller-pinned runtime record digest")
	material := f.String("materialization", "", "materialization record bound by runtime evidence")
	space := f.String("space", "", "caller-declared candidate space JSON")
	hash := f.String("sha256", "", "caller-pinned exact candidate space digest")
	pkg := f.String("package", "", "verified path certificate to revalidate")
	root := f.String("root", "", "read-only baseline root")
	dest := f.String("out", "", "new materialization directory")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *space == "" || *hash == "" {
		return errors.New("candidate space and digest required")
	}
	raw, err := read(*space, validation.MaxBytes)
	if err != nil {
		return err
	}
	prepared, err := candidate.Prepare(raw, *hash)
	if err != nil {
		return err
	}
	if command == "candidate-prepare" {
		if *pkg != "" || *root != "" || *dest != "" || *evidence != "" || *evidenceHash != "" || *material != "" {
			return errors.New("materialization flags are not prepare flags")
		}
		_, err = out.Write(prepared.Authority())
		return err
	}
	if *pkg == "" || *root == "" || (command == "candidate-materialize" && *dest == "") {
		return errors.New("package, baseline root and new output required")
	}
	if command == "candidate-materialize" && (*evidence != "" || *evidenceHash != "" || *material != "") {
		return errors.New("runtime flags are not materialization flags")
	}
	if command == "candidate-bind-runtime" && (*dest != "" || *evidence == "" || *evidenceHash == "" || *material == "") {
		return errors.New("runtime evidence, digest and materialization required; stdout only")
	}
	raw, err = read(*pkg, validation.MaxBytes)
	if err != nil {
		return err
	}
	if command == "candidate-bind-runtime" {
		eraw, e := read(*evidence, validation.MaxBytes)
		if e != nil {
			return e
		}
		mraw, e := read(*material, validation.MaxBytes)
		if e != nil {
			return e
		}
		bound, e := prepared.BindRuntime(ctx, raw, mraw, eraw, *evidenceHash, *root)
		if e != nil {
			return e
		}
		return output(out, bound)
	}
	result, err := prepared.Materialize(ctx, raw, *root, *dest)
	if err != nil {
		return err
	}
	return output(out, result)
}
