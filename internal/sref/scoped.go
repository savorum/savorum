package sref

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
)

// ScopedExtensionsMember is where Savorum stores extension members it found on
// objects inside a recipe. SREF requires preserving unknown extensions at the
// scope they were found; rather than a bag on every scope, they are held
// together and re-attached by address on the way out, so this container is
// never written to SREF. It is still a conforming extension name under
// Savorum's domain, `savorum.app`.
const ScopedExtensionsMember = "x-app.savorum.scoped-extensions"

// Addresses name an object inside a recipe. An object carrying a recipe-local
// identifier is addressed by it, so the address survives reordering and simply
// stops resolving when the object is deleted. Everything else is addressed
// relative to its nearest identified ancestor:
//
//	ingredient:flour
//	ingredient:flour/quantity/terms/0
//	step:knead
//	yield
//
// identifiedCollections maps a member holding such objects to the prefix its
// elements are addressed by.
var identifiedCollections = map[string]string{
	"ingredient_sections":  "section",
	"instruction_sections": "instructions",
	"ingredients":          "ingredient",
	"alternatives":         "ingredient",
	"steps":                "step",
	"assets":               "asset",
	// Timing assertions carry stable IDs, so an extension on one survives
	// reordering.
	"assertions": "timing",
}

// liftScopedExtensions strips extension members from every object inside the
// recipe and returns them keyed by address, leaving a document that decodes
// strictly against the standard members.
func liftScopedExtensions(members map[string]json.RawMessage) (map[string]any, error) {
	body, err := json.Marshal(members)
	if err != nil {
		return nil, fmt.Errorf("encode standard members for extension lifting: %w", err)
	}

	var document map[string]any
	if err := decodeWithNumbers(body, &document); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}

	lifted := map[string]any{}

	for name, value := range document {
		// Top-level extensions are handled by the caller, which has to tell
		// them apart from the standard `extensions` member.
		if isExtensionName(name) || name == StandardExtensionsMember {
			continue
		}

		document[name] = strip(value, "", name, lifted)
	}

	if len(lifted) == 0 {
		return nil, nil
	}

	rebuilt, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode document after extension lifting: %w", err)
	}

	var rewritten map[string]json.RawMessage
	if err := json.Unmarshal(rebuilt, &rewritten); err != nil {
		return nil, fmt.Errorf("decode document after extension lifting: %w", err)
	}

	for name := range members {
		delete(members, name)
	}

	maps.Copy(members, rewritten)

	return lifted, nil
}

// strip walks one value, recording and removing every extension member it
// finds. base is the address of the nearest identified ancestor and member is
// the path from it.
func strip(value any, base, member string, lifted map[string]any) any {
	switch holder := value.(type) {
	case map[string]any:
		return stripMap(holder, base, member, lifted)
	case []any:
		return stripSlice(holder, base, member, lifted)
	default:
		return value
	}
}

func stripMap(holder map[string]any, base, member string, lifted map[string]any) map[string]any {
	address := join(base, member)
	found := map[string]any{}

	for name, child := range holder {
		if isExtensionName(name) {
			found[name] = child
			delete(holder, name)

			continue
		}

		holder[name] = strip(child, base, join(member, name), lifted)
	}

	if len(found) > 0 && address != "" {
		lifted[address] = found
	}

	return holder
}

func stripSlice(holder []any, base, member string, lifted map[string]any) []any {
	collection := lastSegment(member)

	prefix, identified := identifiedCollections[collection]
	for index := range holder {
		if collection == "ingredient_uses" {
			if object, ok := holder[index].(map[string]any); ok {
				if reference, named := object["ingredient_ref"].(string); named {
					holder[index] = strip(object, base, join(member, "ingredient:"+reference), lifted)
					continue
				}
			}
		}

		if identified {
			if object, ok := holder[index].(map[string]any); ok {
				if id, named := object["id"].(string); named {
					holder[index] = strip(object, prefix+":"+id, "", lifted)
					continue
				}
			}
		}

		holder[index] = strip(holder[index], base, join(member, fmt.Sprint(index)), lifted)
	}

	return holder
}

// restoreScopedExtensions puts the lifted members back on the objects they came
// from. An address that no longer resolves is dropped rather than attached to
// something else, which is what an edit between reading and writing can cause.
func restoreScopedExtensions(members map[string]json.RawMessage, lifted map[string]any) error {
	return restoreScopedMembers(members, lifted, false)
}

func restoreScopedMembers(members map[string]json.RawMessage, lifted map[string]any, requireEveryAddress bool) error {
	if len(lifted) == 0 {
		return nil
	}

	body, err := json.Marshal(members)
	if err != nil {
		return fmt.Errorf("encode standard members for extension restore: %w", err)
	}

	var document map[string]any
	if err := decodeWithNumbers(body, &document); err != nil {
		return fmt.Errorf("decode standard members for extension restore: %w", err)
	}

	for address, extensions := range lifted {
		carried, ok := extensions.(map[string]any)
		if !ok {
			if requireEveryAddress {
				return fmt.Errorf("%w: invalid sidecar entry at %s", ErrOpaqueDataDetached, address)
			}

			continue
		}

		target := resolve(document, address)
		if target == nil {
			if requireEveryAddress {
				return fmt.Errorf("%w: %s no longer resolves", ErrOpaqueDataDetached, address)
			}

			continue
		}

		for name, value := range carried {
			if _, collision := target[name]; collision && requireEveryAddress {
				if address != "document/sref" || (name != "version" && name != "unit_registry") {
					return fmt.Errorf("%w: %s member %s is now understood", ErrOpaqueDataDetached, address, name)
				}
			}

			target[name] = value
		}
	}

	rebuilt, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode document after extension restore: %w", err)
	}

	var rewritten map[string]json.RawMessage
	if err := json.Unmarshal(rebuilt, &rewritten); err != nil {
		return fmt.Errorf("decode document after extension restore: %w", err)
	}

	maps.Copy(members, rewritten)

	return nil
}

// resolve finds the object an address names, or nil when the recipe no longer
// contains it.
func resolve(document map[string]any, address string) map[string]any {
	parts := strings.Split(address, "/")

	current := anchor(document, parts[0])
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}

		switch holder := current.(type) {
		case map[string]any:
			current = holder[part]
		case []any:
			if reference, ingredientUse := strings.CutPrefix(part, "ingredient:"); ingredientUse {
				current = nil

				for _, item := range holder {
					if object, ok := item.(map[string]any); ok && object["ingredient_ref"] == reference {
						current = object
						break
					}
				}

				if current == nil {
					return nil
				}

				continue
			}

			var index int
			if _, err := fmt.Sscanf(part, "%d", &index); err != nil || index < 0 || index >= len(holder) {
				return nil
			}

			current = holder[index]
		default:
			return nil
		}
	}

	object, ok := current.(map[string]any)
	if !ok {
		return nil
	}

	return object
}

// anchor finds the object an address's first segment names, which is either an
// identified object anywhere in the recipe or a top-level member.
func anchor(document map[string]any, head string) any {
	if head == "document" {
		return document
	}

	prefix, id, identified := strings.Cut(head, ":")
	if !identified {
		return document[head]
	}

	var found any

	walk(document, func(object map[string]any, member string) {
		if found != nil {
			return
		}

		if identifiedCollections[member] == prefix && object["id"] == id {
			found = object
		}
	})

	return found
}

// walk visits every object in the recipe together with the member name of the
// collection it came from.
func walk(value any, visit func(map[string]any, string)) {
	switch holder := value.(type) {
	case map[string]any:
		for name, child := range holder {
			if list, ok := child.([]any); ok {
				for _, item := range list {
					if object, ok := item.(map[string]any); ok {
						visit(object, name)
					}

					walk(item, visit)
				}

				continue
			}

			walk(child, visit)
		}
	case []any:
		for _, item := range holder {
			walk(item, visit)
		}
	}
}

func join(base, member string) string {
	switch {
	case member == "":
		return base
	case base == "":
		return member
	default:
		return base + "/" + member
	}
}

func lastSegment(member string) string {
	if _, after, ok := strings.CutLast(member, "/"); ok {
		return after
	}

	return member
}
