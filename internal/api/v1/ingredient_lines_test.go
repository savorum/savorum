package v1

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAnIngredientLineIsReadByItsLanguage(t *testing.T) {
	t.Parallel()

	handler, _, _ := testAPI(t)

	for _, test := range []struct {
		body       string
		name, unit string
		method     string
	}{
		{`{"text":"2 EL Olivenöl","language":"de"}`, "Olivenöl", "volume.tablespoon.metric.culinary", "extracted"},
		{`{"text":"サラダ油 大さじ2","language":"ja-JP"}`, "サラダ油", "volume.tablespoon.metric.culinary", "extracted"},
		{`{"text":"2 cups flour, sifted","source_locale":"en-US","language":"de"}`, "flour", "volume.cup.us.customary", "extracted"},
		{`{"text":"1 Tasse Milch","language":"de"}`, "1 Tasse Milch", "", "unresolved"},
	} {
		response := request(t, handler, http.MethodPost, "/api/v1/ingredient-lines/parse", []byte(test.body))
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", test.body, response.Code, response.Body.String())
		}

		var read struct {
			Ingredient struct {
				Name     string `json:"name"`
				Quantity *struct {
					Unit string `json:"unit"`
				} `json:"quantity"`
				Normalization *struct {
					Method string `json:"method"`
				} `json:"normalization"`
			} `json:"ingredient"`
		}

		if err := json.Unmarshal(response.Body.Bytes(), &read); err != nil {
			t.Fatal(err)
		}

		unit := ""
		if read.Ingredient.Quantity != nil {
			unit = read.Ingredient.Quantity.Unit
		}

		method := ""
		if read.Ingredient.Normalization != nil {
			method = read.Ingredient.Normalization.Method
		}

		if read.Ingredient.Name != test.name || unit != test.unit || method != test.method {
			t.Errorf("%s read as %q, %q, %q; want %q, %q, %q", test.body, read.Ingredient.Name, unit, method, test.name, test.unit, test.method)
		}
	}
}

func TestAnEmptyIngredientLineIsRefused(t *testing.T) {
	t.Parallel()

	handler, _, _ := testAPI(t)

	if response := request(t, handler, http.MethodPost, "/api/v1/ingredient-lines/parse", []byte(`{"text":""}`)); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", response.Code)
	}
}
