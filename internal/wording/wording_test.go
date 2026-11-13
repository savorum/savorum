package wording_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/savorum/savorum/internal/wording"
)

// The web and native clients read the same cases.
func TestOnlyAmountsInWordingFollowTheBatch(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/states-amount.json")
	if err != nil {
		t.Fatal(err)
	}

	var cases []struct {
		Locale       string `json:"locale"`
		Text         string `json:"text"`
		StatesAmount bool   `json:"states_amount"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}

	for _, test := range cases {
		if got := wording.StatesAmount(test.Text, test.Locale); got != test.StatesAmount {
			t.Errorf("%s %q states an amount = %t, want %t", test.Locale, test.Text, got, test.StatesAmount)
		}
	}
}
