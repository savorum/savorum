package strictjson

import (
	"encoding/json"
	"strings"
	"testing"
)

func FuzzStrictJSON(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{"version":"0.3.0","recipe":{"title":"Soup"}}`),
		[]byte(`{"a":1,"a":2}`),
		[]byte(`[[[[0]]]]`),
		[]byte(`{"amount":999999999999999999999999999999999999}`),
		{0xff, 0xfe},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			return
		}

		var decoded any
		if err := Decode(data, &decoded); err != nil {
			return
		}

		canonical, err := json.Marshal(decoded)
		if err != nil {
			t.Fatal(err)
		}

		if err := Validate(canonical); err != nil {
			t.Fatalf("successful decode produced invalid JSON: %v", err)
		}
	})
}

func TestDeepJSONAndLargeNumberFailPredictably(t *testing.T) {
	deep := strings.Repeat("[", maxJSONDepth+1) + "0" + strings.Repeat("]", maxJSONDepth+1)
	if err := Validate([]byte(deep)); err == nil {
		t.Fatal("accepted JSON beyond the depth limit")
	}

	allowed := strings.Repeat("[", maxJSONDepth) + `"[\"{}]"` + strings.Repeat("]", maxJSONDepth)
	if err := Validate([]byte(allowed)); err != nil {
		t.Fatalf("rejected JSON at the depth limit: %v", err)
	}

	var value any

	large := []byte(`{"amount":` + strings.Repeat("9", 100_000) + `}`)
	_ = Decode(large, &value)
}
