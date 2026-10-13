package main

import "testing"

func TestStorageStatusContractIsPublished(t *testing.T) {
	definition, err := schemaDefinition()
	if err != nil {
		t.Fatal(err)
	}

	operation := definition.OpenAPI().Paths["/api/v1/storage-status"]
	if operation == nil || operation.Get == nil || operation.Get.OperationID != "get-storage-status" {
		t.Fatal("storage status operation missing")
	}

	schema := definition.OpenAPI().Components.Schemas.Map()["CustomerStorageStatus"]
	if schema == nil || schema.Properties["available_bytes"] == nil || schema.Properties["used_bytes"] == nil || schema.Properties["support_url"] == nil {
		t.Fatal("storage observation contract missing")
	}
}
