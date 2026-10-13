package main

import (
	"strings"
	"testing"
	"unicode"

	"github.com/danielgtaylor/huma/v2"
)

// summaryBudget is the longest summary that stays one sidebar title.

const summaryBudget = 30

// TestOperationNamingIsConsistent guards the api/README.md naming rules.

func TestOperationNamingIsConsistent(t *testing.T) {
	t.Parallel()

	definition, err := schemaDefinition()
	if err != nil {
		t.Fatal(err)
	}

	document := definition.OpenAPI()
	declared := map[string]struct{}{}

	for _, tag := range document.Tags {
		if tag.Description == "" {
			t.Errorf("tag %q has no description", tag.Name)
		}

		declared[tag.Name] = struct{}{}
	}

	verbs := map[string]struct{}{}
	for _, verb := range []string{
		"list", "get", "create", "replace", "update", "delete", "trash", "restore",
		"merge", "import", "export", "begin", "finish", "revoke", "cancel", "resume",
		"apply", "commit", "head", "login", "logout", "select", "rename", "generate",
		"recover", "set", "remove", "accept", "register", "unlink", "force", "preview",
		"complete", "start", "empty", "change", "rotate",
	} {
		verbs[verb] = struct{}{}
	}

	summaries := map[string]string{}

	for path, item := range document.Paths {
		for method, operation := range map[string]*huma.Operation{
			"GET": item.Get, "PUT": item.Put, "POST": item.Post,
			"DELETE": item.Delete, "PATCH": item.Patch, "HEAD": item.Head,
		} {
			if operation == nil {
				continue
			}

			where := method + " " + path
			if _, ok := verbs[strings.SplitN(operation.OperationID, "-", 2)[0]]; !ok {
				t.Errorf("%s: operation ID %q does not start with an approved verb", where, operation.OperationID)
			}

			switch {
			case operation.Summary == "":
				t.Errorf("%s: no summary", where)
			case strings.HasSuffix(operation.Summary, "."):
				t.Errorf("%s: summary %q ends with a period", where, operation.Summary)
			case unicode.IsLower(rune(operation.Summary[0])):
				t.Errorf("%s: summary %q is not capitalised", where, operation.Summary)
			case operation.Summary == strings.ReplaceAll(operation.OperationID, "-", " "):
				t.Errorf("%s: summary %q is derived from its operation ID", where, operation.Summary)
			case len(operation.Summary) > summaryBudget:
				t.Errorf("%s: summary %q is %d characters, over the %d-character title budget",
					where, operation.Summary, len(operation.Summary), summaryBudget)
			}

			if previous, seen := summaries[operation.Summary]; seen {
				t.Errorf("%s: summary %q is already used by %s", where, operation.Summary, previous)
			}

			summaries[operation.Summary] = where

			if len(operation.Tags) == 0 {
				t.Errorf("%s: no tag", where)
			}

			for _, tag := range operation.Tags {
				if _, ok := declared[tag]; !ok {
					t.Errorf("%s: tag %q is not declared in the top-level tag list", where, tag)
				}
			}
		}
	}
}
