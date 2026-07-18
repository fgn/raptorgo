package strictjson

import (
	"strings"
	"testing"
)

func TestCheckRejectsDuplicateNamesAtEveryDepth(t *testing.T) {
	tests := []string{
		`{"v":1,"v":1}`,
		`{"v":1,"\u0076":1}`,
		`{"outer":{"v":1,"v":2}}`,
		`[{"v":1,"v":2}]`,
	}
	for _, input := range tests {
		if err := Check([]byte(input)); err == nil || !strings.Contains(err.Error(), `duplicate object name "v"`) {
			t.Errorf("Check(%s) = %v", input, err)
		}
	}
}

func TestCheckAcceptsDistinctNamesAndRejectsTrailingValues(t *testing.T) {
	if err := Check([]byte(`{"v":1,"nested":{"v":2},"values":[1,true,null]}`)); err != nil {
		t.Fatal(err)
	}
	if err := Check([]byte(`{} {}`)); err == nil || !strings.Contains(err.Error(), "multiple JSON values") {
		t.Fatalf("trailing-value error = %v", err)
	}
}
