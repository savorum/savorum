package sref

import (
	"encoding/json"
	"fmt"
	"maps"
)

// knownObjectMembers is the standard SREF subset this build understands. A
// compatible newer format may add members to these objects; those additions
// are lifted whole and never traversed or interpreted.
var knownObjectMembers = map[string]map[string]bool{
	"document":           set("sref", "id", "title", "description", "language", "source_locale", "authors", "source", "yield", "yields", "nutrition", "dietary_claims", "allergen_declarations", "times", "categories", "courses", "keywords", "cuisines", "cooking_methods", "published", "modified", "notes", "ingredient_sections", "instruction_sections", "assets", "image_refs", "primary_image", "variants", "equipment", "translations", StandardExtensionsMember),
	"author":             set("name", "kind", "kind_label", "url"),
	"sref":               set("version", "unit_registry"),
	"source":             set("name", "url", "application", "source_id", "retrieved_at", "attribution"),
	"yield":              set("text", "quantity", "label"),
	"times":              set("prep", "cook", "additional", "total", "assertions"),
	"duration":           set("value", "min", "max", "approximate", "text", "source_text"),
	"timing":             set("id", "kind", "label", "duration", "attention", "note"),
	"section":            set("id", "title", "ingredients"),
	"ingredient":         set("id", "name", "quantity", "note", "source_text", "normalization", "package_size", "temperature", "optional", "alternatives", "relation", "recipe"),
	"normalization":      set("method", "review", "warnings"),
	"quantity":           set("kind", "amount", "unit", "terms", "options", "text"),
	"simpleQuantity":     set("kind", "amount", "unit"),
	"alternativeOption":  set("kind", "amount", "unit", "terms"),
	"amount":             set("value", "min", "max", "approximate"),
	"temperature":        set("amount", "unit", "text", "purpose", "note", "source_text"),
	"temperaturePurpose": set("kind", "label"),
	"instructions":       set("id", "title", "steps"),
	"step":               set("id", "text", "ingredient_uses", "image_refs", "timing_refs", "temperatures"),
	"ingredientUse":      set("ingredient_ref", "quantity"),
	"asset":              set("id", "path", "media_type", "sha256", "size", "role", "alt", "caption", "creator", "credit", "rights", "license", "source_url"),
	"assetLicense":       set("name", "url"),
	"variant":            set("id", "title", "text"),
	"nutritionStatement": set("basis", "nutrients", "note"),
	"nutritionBasis":     set("kind", "text", "quantity"),
	"nutrient":           set("nutrient", "label", "amount", "unit", "source_text", "text"),
	"dietaryClaim":       set("text", "suitability", "diet", "condition", "variant_ref"),
	"allergenClaim":      set("text", "substance", "presence", "condition", "variant_ref"),
	"recipeReference":    set("title", "recipe_id", "url"),
	"equipment":          set("name", "quantity", "note"),
	"translation":        set("relation", "language", "title", "recipe_id", "url"),
}

func set(names ...string) map[string]bool {
	result := make(map[string]bool, len(names))
	for _, name := range names {
		result[name] = true
	}

	return result
}

func liftOpaqueStandardMembers(members map[string]json.RawMessage) (map[string]any, error) {
	body, err := json.Marshal(members)
	if err != nil {
		return nil, fmt.Errorf("encode standard members for opaque-member lifting: %w", err)
	}

	var document map[string]any
	if err := decodeWithNumbers(body, &document); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}

	lifted := map[string]any{}
	stripUnknown(document, "", "document", "document", lifted)

	if len(lifted) == 0 {
		return nil, nil
	}

	rebuilt, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode document after opaque-member lifting: %w", err)
	}

	var rewritten map[string]json.RawMessage
	if err := json.Unmarshal(rebuilt, &rewritten); err != nil {
		return nil, fmt.Errorf("decode document after opaque-member lifting: %w", err)
	}

	for name := range members {
		delete(members, name)
	}

	maps.Copy(members, rewritten)

	return lifted, nil
}

func stripUnknown(value any, base, member, context string, lifted map[string]any) any {
	switch holder := value.(type) {
	case map[string]any:
		return stripUnknownMap(holder, base, member, context, lifted)
	case []any:
		return stripUnknownSlice(holder, base, member, lifted)
	default:
		return value
	}
}

func stripUnknownMap(holder map[string]any, base, member, context string, lifted map[string]any) map[string]any {
	known, understood := knownObjectMembers[context]
	if !understood {
		return holder
	}

	address := join(base, member)
	found := map[string]any{}

	for name, child := range holder {
		if context == "document" && (isExtensionName(name) || name == StandardExtensionsMember) {
			continue
		}

		if !known[name] {
			found[name] = child
			delete(holder, name)

			continue
		}

		holder[name] = stripUnknown(child, base, join(member, name), childContext(context, name), lifted)
	}

	if len(found) > 0 {
		lifted[address] = found
	}

	return holder
}

func stripUnknownSlice(holder []any, base, member string, lifted map[string]any) []any {
	collection := lastSegment(member)
	prefix, identified := identifiedCollections[collection]

	itemContext := collectionContext(collection)
	for index := range holder {
		if collection == "ingredient_uses" {
			if object, ok := holder[index].(map[string]any); ok {
				if reference, named := object["ingredient_ref"].(string); named {
					holder[index] = stripUnknown(object, base, join(member, "ingredient:"+reference), itemContext, lifted)
					continue
				}
			}
		}

		if identified {
			if object, ok := holder[index].(map[string]any); ok {
				if id, named := object["id"].(string); named {
					holder[index] = stripUnknown(object, prefix+":"+id, "", itemContext, lifted)
					continue
				}
			}
		}

		holder[index] = stripUnknown(holder[index], base, join(member, fmt.Sprint(index)), itemContext, lifted)
	}

	return holder
}

func childContext(parent, name string) string {
	switch name {
	case "sref", "source", "yield", "times", "normalization", "quantity", "temperature":
		return name
	case "purpose":
		if parent == "temperature" {
			return "temperaturePurpose"
		}

		return ""
	case "package_size":
		return "simpleQuantity"
	case "basis":
		return "nutritionBasis"
	case "license":
		return "assetLicense"
	case "recipe":
		if parent == "ingredient" {
			return "recipeReference"
		}

		return ""
	case "amount":
		return "amount"
	case "prep", "cook", "additional", "total", "duration":
		return "duration"
	default:
		return ""
	}
}

func collectionContext(name string) string {
	switch name {
	case "ingredient_sections":
		return "section"
	case "instruction_sections":
		return "instructions"
	case "ingredients", "alternatives":
		return "ingredient"
	case "steps":
		return "step"
	case "assertions":
		return "timing"
	case "assets":
		return "asset"
	case "terms":
		return "simpleQuantity"
	case "options":
		return "alternativeOption"
	case "temperatures":
		return "temperature"
	case "ingredient_uses":
		return "ingredientUse"
	case "authors":
		return "author"
	case "variants":
		return "variant"
	case "equipment":
		return "equipment"
	case "translations":
		return "translation"
	case "yields":
		return "yield"
	case "nutrition":
		return "nutritionStatement"
	case "nutrients":
		return "nutrient"
	case "dietary_claims":
		return "dietaryClaim"
	case "allergen_declarations":
		return "allergenClaim"
	default:
		return ""
	}
}
