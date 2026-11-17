package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shared examples in schema/examples are the same data every service tests against.
// valid/ must decode; invalid/schema-* must be rejected by the generated validation;
// invalid/rule-* break cross-field rules that agents-python checks, so they decode here.
const examples = "../../../../schema/examples"

func TestPlanExamples(t *testing.T) {
	checkExamples(t, "plan", func(raw []byte) error { var p Plan; return json.Unmarshal(raw, &p) })
}

func TestCreatePlanRequestExamples(t *testing.T) {
	checkExamples(t, "create-plan.request", func(raw []byte) error { var r CreatePlanRequest; return json.Unmarshal(raw, &r) })
}

func checkExamples(t *testing.T, name string, decode func([]byte) error) {
	t.Helper()
	for _, kind := range []string{"valid", "invalid"} {
		files, err := filepath.Glob(filepath.Join(examples, name, kind, "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		if kind == "valid" && len(files) == 0 {
			t.Fatalf("no examples found under %s/%s/valid", examples, name)
		}
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			base := filepath.Base(f)
			wantOK := kind == "valid" || strings.HasPrefix(base, "rule-")
			err = decode(raw)
			if wantOK && err != nil {
				t.Errorf("%s/%s/%s: want accepted, got %v", name, kind, base, err)
			}
			if !wantOK && err == nil {
				t.Errorf("%s/%s/%s: want rejected, got accepted", name, kind, base)
			}
		}
	}
}
