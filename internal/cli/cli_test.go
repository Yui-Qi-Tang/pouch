package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/ahe"
)

func TestPublicationStatusKeepsWriteBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    ahe.Publication
		err  error
		want string
	}{
		{"success", ahe.Publication{}, nil, "ADMITTED_AND_READ_BACK"},
		{"prewrite", ahe.Publication{}, errors.New("approval mismatch"), "NOT_ADMITTED"},
		{"lostack", ahe.Publication{}, ahe.ErrDeliveryUnknown, "DELIVERY_UNKNOWN"},
		{"readback", ahe.Publication{Admission: json.RawMessage(`{}`)}, errors.New("readback failed"), "ADMISSION_RECEIVED_READBACK_INCOMPLETE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := publicationStatus(tc.p, tc.err); got != tc.want {
				t.Fatalf("%s != %s", got, tc.want)
			}
		})
	}
}
func TestCLIUsageDoesNotCreateOutput(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "result")
	var out, errs bytes.Buffer
	if got := Run(context.Background(), []string{"solve", "--out", dir}, &out, &errs); got != 2 {
		t.Fatalf("exit %d", got)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output created: %v", err)
	}
}
func TestExternalApprovalRejectsUnknownFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "approval.json")
	if err := os.WriteFile(p, []byte(`{"request_id":"r","subject":"s","display_sha256":"d","package_sha256":"p","reason":"reviewed","automatic":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	var a ahe.Approval
	if err := configJSON(p, &a); err == nil {
		t.Fatal("unknown approval authority field accepted")
	}
}

func TestExternalApprovalRejectsAmbiguousKeys(t *testing.T) {
	for _, raw := range []string{`{"subject":"approved","subject":"different"}`, `{"subject":"approved","Subject":"different"}`} {
		if err := uniqueConfigKeys([]byte(raw)); err == nil {
			t.Fatal("ambiguous review key accepted")
		}
	}
	if err := uniqueConfigKeys([]byte(`{"subject":"x","nested":{"a":[1,2]}}`)); err != nil {
		t.Fatal(err)
	}
}
