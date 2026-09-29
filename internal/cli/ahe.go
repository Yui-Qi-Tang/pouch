package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"strings"
	"time"

	"github.com/Yui-Qi-Tang/pouch/internal/ahe"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

type profileConfig struct {
	Command ahe.Command       `json:"command"`
	Pins    map[string]string `json:"pins"`
}
type connectionConfig struct {
	Query     profileConfig `json:"query"`
	Intake    profileConfig `json:"intake"`
	Endpoints profileConfig `json:"endpoints"`
}

func configJSON(path string, v any) error {
	raw, e := read(path, validation.MaxBytes)
	if e != nil {
		return e
	}
	if err := uniqueConfigKeys(raw); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("one config JSON value required")
	}
	return nil
}
func connect(ctx context.Context, c profileConfig, kind ahe.ProfileKind) (*ahe.Client, *ahe.Profile, error) {
	client, e := ahe.Start(ctx, c.Command)
	if e != nil {
		return nil, nil, e
	}
	p, e := client.Bind(kind, c.Pins)
	if e != nil {
		client.Close()
		return nil, nil, e
	}
	return client, p, nil
}
func aheCommand(ctx context.Context, command string, args []string, out, errout io.Writer) error {
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(errout)
	config := f.String("config", "", "local launch config; never copied into result artifacts")
	requestFile := f.String("request", "", "bounded snapshot request JSON")
	authorityFile := f.String("authority", "", "caller-owned authority JSON")
	hash := f.String("authority-sha256", "", "caller-pinned digest")
	requestID := f.String("request-id", "", "caller-selected request ID")
	packageFile := f.String("package", "", "validated candidate package JSON")
	proposal := f.String("proposal-id", "", "existing exact proposal occurrence for admission")
	approvalFile := f.String("approval", "", "external exact-review approval JSON")
	intakeID := f.String("intake-request-id", "", "explicit unique pending submission request ID")
	observed := f.String("observed-at", "", "explicit RFC3339 timestamp for submitting the result record")
	timeout := f.Duration("timeout", time.Minute, "complete command budget")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 0 || *config == "" || *timeout <= 0 {
		return errors.New("config and positive timeout required")
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	if command == "ahe-discover" {
		var cfg ahe.Command
		if e := configJSON(*config, &cfg); e != nil {
			return e
		}
		client, e := ahe.Start(ctx, cfg)
		if e != nil {
			return e
		}
		defer client.Close()
		type tool struct {
			Name   string          `json:"name"`
			SHA256 string          `json:"input_schema_sha256"`
			Schema json.RawMessage `json:"input_schema"`
		}
		list := []tool{}
		for _, t := range client.Tools() {
			list = append(list, tool{t.Name, t.SchemaSHA256(), t.InputSchema})
		}
		return output(out, list)
	}
	var cfg connectionConfig
	if e := configJSON(*config, &cfg); e != nil {
		return e
	}
	queryClient, query, e := connect(ctx, cfg.Query, ahe.ProfileQuery)
	if e != nil {
		return e
	}
	defer queryClient.Close()
	if command == "ahe-read" {
		var req ahe.ReadRequest
		if e := configJSON(*requestFile, &req); e != nil {
			return e
		}
		snapshot, e := ahe.ReadSnapshot(ctx, query, req)
		if e != nil {
			return e
		}
		return output(out, snapshot)
	}
	raw, e := read(*authorityFile, validation.MaxBytes)
	if e != nil {
		return e
	}
	pkg, e := read(*packageFile, validation.MaxBytes)
	if e != nil {
		return e
	}
	prepared, e := ahe.Prepare(ctx, query, raw, *hash, *requestID, pkg)
	if e != nil {
		return e
	}
	endClient, endpoints, e := connect(ctx, cfg.Endpoints, ahe.ProfileEndpointReviewer)
	if e != nil {
		return e
	}
	defer endClient.Close()
	if command == "ahe-stage" {
		at, e := time.Parse(time.RFC3339Nano, *observed)
		if e != nil {
			return errors.New("explicit observed-at RFC3339 timestamp required")
		}
		intakeClient, intake, e := connect(ctx, cfg.Intake, ahe.ProfileIntake)
		if e != nil {
			return e
		}
		defer intakeClient.Close()
		pending, e := prepared.Submit(ctx, intake, *intakeID, at)
		if e != nil {
			if oe := output(out, struct {
				Status  string      `json:"status"`
				Pending ahe.Pending `json:"pending"`
			}{"RECONCILE_PENDING_SUBMISSION", pending}); oe != nil {
				return oe
			}
			return e
		}
		review, e := prepared.Review(ctx, endpoints, pending.ProposalID)
		response := struct {
			Status  string      `json:"status"`
			Pending ahe.Pending `json:"pending"`
			Review  ahe.Review  `json:"review"`
		}{"AWAITING_EXTERNAL_APPROVAL", pending, review}
		if e != nil {
			response.Status = "REVIEW_INCOMPLETE"
		}
		if oe := output(out, response); oe != nil {
			return oe
		}
		return e
	}
	if command == "ahe-admit" {
		var approval ahe.Approval
		if e := configJSON(*approvalFile, &approval); e != nil {
			return e
		}
		// Refresh native review, then require the caller's previously inspected exact display.
		review, e := prepared.Review(ctx, endpoints, *proposal)
		if e != nil {
			return e
		}
		publication, e := prepared.Admit(ctx, endpoints, query, review, approval)
		status := publicationStatus(publication, e)
		if oe := output(out, struct {
			Status      string          `json:"status"`
			Publication ahe.Publication `json:"publication"`
		}{status, publication}); oe != nil {
			return oe
		}
		return e
	}
	return errors.New("unknown AHE operation")
}

// Preserve whether the write was attempted/acknowledged; a pre-write approval
// rejection is not an uncertain admission and must not request reconciliation.
func publicationStatus(p ahe.Publication, err error) string {
	if err == nil {
		return "ADMITTED_AND_READ_BACK"
	}
	if errors.Is(err, ahe.ErrDeliveryUnknown) {
		return "DELIVERY_UNKNOWN"
	}
	if len(p.Admission) != 0 {
		return "ADMISSION_RECEIVED_READBACK_INCOMPLETE"
	}
	return "NOT_ADMITTED"
}

// Local approval/config files must not contain ambiguous duplicate or aliased keys.
func uniqueConfigKeys(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var value func(int) error
	value = func(depth int) error {
		if depth > 32 {
			return errors.New("config nesting limit")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok {
					return errors.New("config object key required")
				}
				name = strings.ToLower(name)
				if seen[name] {
					return errors.New("duplicate or aliased config key")
				}
				seen[name] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid config delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("one config JSON value required")
	}
	return nil
}
