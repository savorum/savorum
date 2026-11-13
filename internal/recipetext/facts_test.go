package recipetext

import (
	"testing"
	"time"
)

func TestFactRecognizesLabeledRecipeFacts(t *testing.T) {
	for _, test := range []struct {
		line, kind, value string
	}{
		{"Serves: 4", FactYield, "4"},
		{"Serves 4", FactYield, "4"},
		{"Makes 24 cookies", FactYield, "24 cookies"},
		{"SERVINGS: -3", FactYield, "-3"},
		{"Prep time: 99999 hours", FactPrep, "99999 hours"},
		{"Prep: 10 min", FactPrep, "10 min"},
		{"Cook time:  1 hr 30 min ", FactCook, "1 hr 30 min"},
		{"Total time: about an hour", FactTotal, "about an hour"},
		{"Vorbereitungszeit: 20 Minuten", FactPrep, "20 Minuten"},
		{"Portionen: 4", FactYield, "4"},
		{"調理時間：30分", FactCook, "30分"},
		{"Serves", "", ""},
		{"Serves:", "", ""},
		{"Makes a great snack", "", ""},
		{"Prep the vegetables first", "", ""},
		{"Preparation: chop the onions", "", ""},
		{"Preparation time: 15 min", FactPrep, "15 min"},
		{"Totally worth it: yes", "", ""},
	} {
		kind, value := Fact(test.line)
		if kind != test.kind || value != test.value {
			t.Errorf("Fact(%q) = %q, %q; want %q, %q", test.line, kind, value, test.kind, test.value)
		}
	}
}

func TestDurationReadsOnlyAmountsAndUnits(t *testing.T) {
	for _, test := range []struct {
		text string
		want time.Duration
		ok   bool
	}{
		{"20 min", 20 * time.Minute, true},
		{"1 hr 30 mins", 90 * time.Minute, true},
		{"1.5 hours", 90 * time.Minute, true},
		{"1,5 Stunden", 90 * time.Minute, true},
		{"99999 hours", 99999 * time.Hour, true},
		{"1時間30分", 90 * time.Minute, true},
		{"2 h, 10 min", 130 * time.Minute, true},
		{"banana", 0, false},
		{"about 20 min", 0, false},
		{"20 minutes or so", 0, false},
		{"20", 0, false},
		{"0 min", 0, false},
		{"20 minx", 0, false},
		{"0.5 sec", 0, false},
	} {
		got, ok := Duration(test.text)
		if got != test.want || ok != test.ok {
			t.Errorf("Duration(%q) = %v, %v; want %v, %v", test.text, got, ok, test.want, test.ok)
		}
	}
}

func TestServingLine(t *testing.T) {
	for line, want := range map[string]bool{
		"6 people": true, "Pro 4 osoby": true, "4-6 porții": true, "2 人份": true,
		"6 eggs": false, "Serves 4": false, "4 people, divided": false,
	} {
		if got := ServingLine(line); got != want {
			t.Errorf("ServingLine(%q) = %v", line, got)
		}
	}
}
