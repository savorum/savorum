package sref

import (
	"reflect"
	"strings"
)

// The wire types own admitted fields, including fields restored from preservation extensions.
func unknownWireMember(value any, target reflect.Type) string {
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}

	switch holder := value.(type) {
	case []any:
		if target.Kind() != reflect.Slice && target.Kind() != reflect.Array {
			return ""
		}

		for _, item := range holder {
			if member := unknownWireMember(item, target.Elem()); member != "" {
				return member
			}
		}
	case map[string]any:
		if target.Kind() != reflect.Struct {
			return ""
		}

		fields := reflect.VisibleFields(target)
		for name, child := range holder {
			field, found := wireField(fields, name)
			if !found {
				return name
			}

			if member := unknownWireMember(child, field.Type); member != "" {
				return name + "." + member
			}
		}
	}

	return ""
}

func wireField(fields []reflect.StructField, name string) (reflect.StructField, bool) {
	for _, field := range fields {
		if !field.IsExported() {
			continue
		}

		label, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if label == "-" {
			continue
		}

		if label == "" {
			label = field.Name
		}

		if strings.EqualFold(name, label) {
			return field, true
		}
	}

	return reflect.StructField{}, false
}
