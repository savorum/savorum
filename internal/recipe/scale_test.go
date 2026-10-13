package recipe

import (
	"math/big"
	"testing"
)

func TestQuantityScaledKeepsEveryStructure(t *testing.T) {
	r := func(value string) *Rational {
		rational := Rational(value)
		return &rational
	}
	double := big.NewRat(2, 1)

	simple, err := Quantity{Kind: QuantitySimple, Amount: &Amount{Value: r("3/4"), Approximate: true}, Unit: "volume.cup.us.customary"}.Scaled(double)
	if err != nil || *simple.Amount.Value != "3/2" || !simple.Amount.Approximate || simple.Unit != "volume.cup.us.customary" {
		t.Fatalf("simple = %#v, %v", simple, err)
	}

	ranged, err := Quantity{Kind: QuantitySimple, Amount: &Amount{Min: r("2"), Max: r("3")}}.Scaled(big.NewRat(1, 2))
	if err != nil || *ranged.Amount.Min != "1" || *ranged.Amount.Max != "3/2" || ranged.Amount.Value != nil {
		t.Fatalf("range = %#v, %v", ranged, err)
	}

	sum, err := Quantity{Kind: QuantitySum, Terms: []SimpleQuantity{
		{Kind: QuantitySimple, Amount: Amount{Value: r("1")}, Unit: "volume.cup.us.customary"},
		{Kind: QuantitySimple, Amount: Amount{Value: r("2")}, Unit: "volume.tablespoon.us.customary"},
	}}.Scaled(double)
	if err != nil || *sum.Terms[0].Amount.Value != "2" || *sum.Terms[1].Amount.Value != "4" {
		t.Fatalf("sum = %#v, %v", sum, err)
	}

	alternatives, err := Quantity{Kind: QuantityAlternatives, Options: []AlternativeOption{
		{Kind: QuantitySimple, Amount: &Amount{Value: r("200")}, Unit: "mass.gram"},
		{Kind: QuantitySum, Terms: []SimpleQuantity{{Kind: QuantitySimple, Amount: Amount{Value: r("1")}, Unit: "volume.cup.us.customary"}}},
	}}.Scaled(double)
	if err != nil || *alternatives.Options[0].Amount.Value != "400" || *alternatives.Options[1].Terms[0].Amount.Value != "2" {
		t.Fatalf("alternatives = %#v, %v", alternatives, err)
	}

	opaque, err := Quantity{Kind: QuantityOpaque, Text: "a handful"}.Scaled(double)
	if err != nil || opaque.Text != "a handful" {
		t.Fatalf("opaque = %#v, %v", opaque, err)
	}

	original := Quantity{Kind: QuantitySimple, Amount: &Amount{Value: r("1")}}
	if _, err := original.Scaled(double); err != nil || *original.Amount.Value != "1" {
		t.Fatalf("original mutated: %#v", original)
	}
}
