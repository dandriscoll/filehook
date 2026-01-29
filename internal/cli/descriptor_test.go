package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBoutiquesDescriptorIsValidJSON(t *testing.T) {
	descriptor := GetBoutiquesDescriptor()

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(descriptor), &parsed); err != nil {
		t.Fatalf("descriptor is not valid JSON: %v", err)
	}
}

func TestBoutiquesDescriptorHasRequiredFields(t *testing.T) {
	descriptor := GetBoutiquesDescriptor()

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(descriptor), &parsed); err != nil {
		t.Fatalf("failed to parse descriptor: %v", err)
	}

	// Check required top-level fields
	requiredFields := []string{"name", "tool-version", "schema-version", "command-line", "description"}
	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("descriptor missing required field: %s", field)
		}
	}

	// Check schema-version value
	if schemaVersion, ok := parsed["schema-version"].(string); ok {
		if schemaVersion == "" {
			t.Error("schema-version is empty")
		}
	} else {
		t.Error("schema-version is not a string")
	}

	// Check command-line value
	if commandLine, ok := parsed["command-line"].(string); ok {
		if !strings.Contains(commandLine, "filehook") {
			t.Error("command-line does not contain 'filehook'")
		}
	} else {
		t.Error("command-line is not a string")
	}
}

func TestBoutiquesDescriptorHasSpecURL(t *testing.T) {
	descriptor := GetBoutiquesDescriptor()

	// Check that the spec URL is embedded in the JSON
	expectedSpecURL := "https://github.com/boutiques/boutiques/blob/master/boutiques/schema/descriptor.schema.json"
	if !strings.Contains(descriptor, expectedSpecURL) {
		t.Errorf("descriptor does not contain spec URL: %s", expectedSpecURL)
	}

	// Verify it's in x-spec-url field
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(descriptor), &parsed); err != nil {
		t.Fatalf("failed to parse descriptor: %v", err)
	}

	if specURL, ok := parsed["x-spec-url"].(string); ok {
		if specURL != expectedSpecURL {
			t.Errorf("x-spec-url mismatch: got %s, want %s", specURL, expectedSpecURL)
		}
	} else {
		t.Error("x-spec-url field not found or not a string")
	}
}

func TestBoutiquesDescriptorHasInputs(t *testing.T) {
	descriptor := GetBoutiquesDescriptor()

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(descriptor), &parsed); err != nil {
		t.Fatalf("failed to parse descriptor: %v", err)
	}

	inputs, ok := parsed["inputs"].([]interface{})
	if !ok {
		t.Fatal("inputs is not an array")
	}

	if len(inputs) == 0 {
		t.Error("inputs array is empty")
	}

	// Check for expected input IDs
	expectedInputIDs := []string{"command", "config", "directory", "json_output", "dry_run", "debug"}
	foundIDs := make(map[string]bool)

	for _, input := range inputs {
		inputMap, ok := input.(map[string]interface{})
		if !ok {
			t.Error("input is not an object")
			continue
		}

		id, ok := inputMap["id"].(string)
		if !ok {
			t.Error("input missing 'id' field")
			continue
		}
		foundIDs[id] = true

		// Verify required input fields
		requiredInputFields := []string{"name", "type", "description"}
		for _, field := range requiredInputFields {
			if _, ok := inputMap[field]; !ok {
				t.Errorf("input %q missing required field: %s", id, field)
			}
		}

		// Verify value-key is present
		if _, ok := inputMap["value-key"]; !ok {
			t.Errorf("input %q missing value-key", id)
		}
	}

	for _, expectedID := range expectedInputIDs {
		if !foundIDs[expectedID] {
			t.Errorf("expected input ID not found: %s", expectedID)
		}
	}
}

func TestBoutiquesDescriptorHasOutputFiles(t *testing.T) {
	descriptor := GetBoutiquesDescriptor()

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(descriptor), &parsed); err != nil {
		t.Fatalf("failed to parse descriptor: %v", err)
	}

	outputs, ok := parsed["output-files"].([]interface{})
	if !ok {
		t.Fatal("output-files is not an array")
	}

	if len(outputs) == 0 {
		t.Error("output-files array is empty")
	}

	// Check for expected output IDs
	expectedOutputIDs := []string{"state_directory", "queue_database", "debug_log"}
	foundIDs := make(map[string]bool)

	for _, output := range outputs {
		outputMap, ok := output.(map[string]interface{})
		if !ok {
			t.Error("output is not an object")
			continue
		}

		id, ok := outputMap["id"].(string)
		if !ok {
			t.Error("output missing 'id' field")
			continue
		}
		foundIDs[id] = true

		// Verify path-template is present
		if _, ok := outputMap["path-template"]; !ok {
			t.Errorf("output %q missing path-template", id)
		}
	}

	for _, expectedID := range expectedOutputIDs {
		if !foundIDs[expectedID] {
			t.Errorf("expected output ID not found: %s", expectedID)
		}
	}
}

func TestBoutiquesDescriptorName(t *testing.T) {
	descriptor := GetBoutiquesDescriptor()

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(descriptor), &parsed); err != nil {
		t.Fatalf("failed to parse descriptor: %v", err)
	}

	name, ok := parsed["name"].(string)
	if !ok {
		t.Fatal("name is not a string")
	}

	if name != "filehook" {
		t.Errorf("expected name 'filehook', got %q", name)
	}
}

func TestBoutiquesDescriptorInputTypes(t *testing.T) {
	descriptor := GetBoutiquesDescriptor()

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(descriptor), &parsed); err != nil {
		t.Fatalf("failed to parse descriptor: %v", err)
	}

	inputs, ok := parsed["inputs"].([]interface{})
	if !ok {
		t.Fatal("inputs is not an array")
	}

	// Verify input types are valid Boutiques types
	validTypes := map[string]bool{
		"String": true,
		"File":   true,
		"Flag":   true,
		"Number": true,
	}

	for _, input := range inputs {
		inputMap, ok := input.(map[string]interface{})
		if !ok {
			continue
		}

		id := inputMap["id"].(string)
		inputType, ok := inputMap["type"].(string)
		if !ok {
			t.Errorf("input %q has invalid type field", id)
			continue
		}

		if !validTypes[inputType] {
			t.Errorf("input %q has invalid type %q", id, inputType)
		}
	}
}

func TestBoutiquesDescriptorCommandLineFlags(t *testing.T) {
	descriptor := GetBoutiquesDescriptor()

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(descriptor), &parsed); err != nil {
		t.Fatalf("failed to parse descriptor: %v", err)
	}

	inputs, ok := parsed["inputs"].([]interface{})
	if !ok {
		t.Fatal("inputs is not an array")
	}

	// Check that inputs with command-line-flag have correct flags
	expectedFlags := map[string]string{
		"config":      "--config",
		"directory":   "--directory",
		"json_output": "--json",
		"dry_run":     "--dry-run",
		"debug":       "--debug",
	}

	for _, input := range inputs {
		inputMap, ok := input.(map[string]interface{})
		if !ok {
			continue
		}

		id := inputMap["id"].(string)
		if expectedFlag, ok := expectedFlags[id]; ok {
			actualFlag, hasFlag := inputMap["command-line-flag"].(string)
			if !hasFlag {
				t.Errorf("input %q missing command-line-flag", id)
				continue
			}
			if actualFlag != expectedFlag {
				t.Errorf("input %q has wrong flag: got %q, want %q", id, actualFlag, expectedFlag)
			}
		}
	}
}

func TestBoutiquesDescriptorHasGroups(t *testing.T) {
	descriptor := GetBoutiquesDescriptor()

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(descriptor), &parsed); err != nil {
		t.Fatalf("failed to parse descriptor: %v", err)
	}

	groups, ok := parsed["groups"].([]interface{})
	if !ok {
		t.Fatal("groups is not an array")
	}

	if len(groups) == 0 {
		t.Error("groups array is empty")
	}

	// Check for expected group IDs
	expectedGroupIDs := []string{"subcommands", "global_options"}
	foundIDs := make(map[string]bool)

	for _, group := range groups {
		groupMap, ok := group.(map[string]interface{})
		if !ok {
			t.Error("group is not an object")
			continue
		}

		id, ok := groupMap["id"].(string)
		if !ok {
			t.Error("group missing 'id' field")
			continue
		}
		foundIDs[id] = true

		// Verify members array exists
		if _, ok := groupMap["members"]; !ok {
			t.Errorf("group %q missing members", id)
		}
	}

	for _, expectedID := range expectedGroupIDs {
		if !foundIDs[expectedID] {
			t.Errorf("expected group ID not found: %s", expectedID)
		}
	}
}

func TestBoutiquesDescriptorCustomSubcommands(t *testing.T) {
	descriptor := GetBoutiquesDescriptor()

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(descriptor), &parsed); err != nil {
		t.Fatalf("failed to parse descriptor: %v", err)
	}

	custom, ok := parsed["custom"].(map[string]interface{})
	if !ok {
		t.Fatal("custom is not an object")
	}

	subcommands, ok := custom["subcommands"].(map[string]interface{})
	if !ok {
		t.Fatal("custom.subcommands is not an object")
	}

	// Check for expected subcommands
	expectedSubcommands := []string{"watch", "run", "status", "state", "queue", "errors", "retry", "config", "init"}
	for _, cmd := range expectedSubcommands {
		if _, ok := subcommands[cmd]; !ok {
			t.Errorf("expected subcommand not found in custom: %s", cmd)
		}
	}
}
