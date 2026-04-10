// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package patch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"

	"sigs.k8s.io/yaml"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/value"
)

// EditorFunc is a callback that takes input YAML content and returns edited content.
// This allows for different editing implementations (file-based, in-memory, etc.)
type EditorFunc func(ctx context.Context, input []byte) ([]byte, error)

// Edit applies patches to fields using the provided editor function.
// It serializes fields to YAML, calls the editor, and updates patch.Set values based on changes.
func Edit(ctx context.Context, res resource.Resource, fields []resource.Field, patches []resource.Field, editor EditorFunc) ([]resource.Field, error) {
	return edit(ctx, res, fields, patches, false, editor)
}

// Create applies patches to fields for creation using the provided editor function.
// It serializes fields to YAML, calls the editor, and updates patch.Set values based on changes.
func Create(ctx context.Context, res resource.Resource, fields []resource.Field, patches []resource.Field, editor EditorFunc) ([]resource.Field, error) {
	return edit(ctx, res, fields, patches, true, editor)
}

// SaveYAML writes fields to a writer as YAML.
// If create is true, uses create patches; otherwise uses edit patches.
func SaveYAML(res resource.Resource, fields []resource.Field, patches []resource.Field, w io.Writer, create bool) error {
	data, err := saveFields(res, fields, patches, create)
	if err != nil {
		return fmt.Errorf("failed to serialize fields: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("failed to write YAML: %w", err)
	}
	return nil
}

// CommandEditorFunc creates an EditorFunc that runs a shell command with YAML on stdin
// and reads the edited YAML from stdout.
func CommandEditorFunc(stdio config.Stdio, command string) EditorFunc {
	return func(ctx context.Context, input []byte) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Stdin = bytes.NewReader(input)
		cmd.Stderr = stdio.Stderr

		output, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("command exited with error: %w", err)
		}
		return output, nil
	}
}

// VisualCommandEditorFunc creates an EditorFunc that uses a temp file and system editor.
func VisualCommandEditorFunc(stdio config.Stdio) (EditorFunc, error) {
	editorCmd, err := getEditor()
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, input []byte) ([]byte, error) {
		tmpfile, err := os.CreateTemp("", "unikraft-edit-*.yaml")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp file: %w", err)
		}
		defer os.Remove(tmpfile.Name())

		if _, err := tmpfile.Write(input); err != nil {
			tmpfile.Close()
			return nil, fmt.Errorf("failed to write temp file: %w", err)
		}
		if err := tmpfile.Close(); err != nil {
			return nil, fmt.Errorf("failed to close temp file: %w", err)
		}

		cmd := exec.CommandContext(ctx, editorCmd, tmpfile.Name())
		cmd.Stdin = stdio.Stdin
		cmd.Stdout = stdio.Stdout
		cmd.Stderr = stdio.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("editor exited with error: %w", err)
		}

		output, err := os.ReadFile(tmpfile.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read edited file: %w", err)
		}
		return output, nil
	}, nil
}

// ContentEditorFunc creates an EditorFunc that ignores input and returns the provided content.
// This is designed to work with kong's FileContentFlag or similar pre-read file content.
func ContentEditorFunc(content []byte) EditorFunc {
	return func(ctx context.Context, input []byte) ([]byte, error) {
		return content, nil
	}
}

// edit is the core implementation that handles both edit and create operations.
func edit(ctx context.Context, res resource.Resource, fields []resource.Field, patches []resource.Field, create bool, editor EditorFunc) ([]resource.Field, error) {
	data, err := saveFields(res, fields, patches, create)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize fields: %w", err)
	}

	editedData, err := editor(ctx, data)
	if err != nil {
		return nil, err
	}
	editedData = bytes.TrimSpace(editedData)
	if len(editedData) == 0 {
		return nil, fmt.Errorf("edited data is empty")
	}

	fields, err = loadFieldPatches(fields, editedData, create)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize edited fields: %w", err)
	}
	return fields, nil
}

// saveFields serializes fields to YAML for editing.
// It collects patch.Set values and merges in any pending patches.
func saveFields(res resource.Resource, fields []resource.Field, patches []resource.Field, create bool) ([]byte, error) {
	// Filter to displayable fields and clear Add/Del templates
	var displayableFields []resource.Field
	if create {
		displayableFields = FilterDisplayableCreateFields(fields)
	} else {
		displayableFields = FilterDisplayableEditFields(fields)
	}

	// Clear Add/Del on all fields - they come from struct tags and aren't real values
	for _, field := range resource.IterFields(displayableFields) {
		if field.Edit != nil {
			field.Edit.Add = nil
			field.Edit.Del = nil
		}
		if field.Create != nil {
			field.Create.Add = nil
			field.Create.Del = nil
		}
	}

	// Merge in pending patches - if Add/Del become non-nil, they were set via --add/--del
	displayableFields = MergePatches(displayableFields, patches)

	// Collect values from patch.Set into a map for YAML serialization
	result := make(map[string]any)
	for key, field := range resource.IterFields(displayableFields) {
		var patch *resource.Patch
		if create {
			patch = field.Create
		} else {
			patch = field.Edit
		}
		if patch == nil || patch.Set == nil {
			continue
		}
		if patch.Add != nil {
			return nil, fmt.Errorf("%s was added, but visual editing does not support this", key)
		}
		if patch.Del != nil {
			return nil, fmt.Errorf("%s was deleted, but visual editing does not support this", key)
		}
		setNestedValue(result, key, normalizeValue(patch.Set))
	}

	yamlBytes, err := yaml.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal fields to YAML: %w", err)
	}

	line := fmt.Sprintf("# %s", res.Type().Name)
	if key := res.Key().String(); key != "" {
		line = line + " " + key
	}
	yamlBytes = append([]byte(line+"\n"), yamlBytes...)
	return yamlBytes, nil
}

// loadFieldPatches parses edited YAML and updates patch.Set values for changed fields.
func loadFieldPatches(fields []resource.Field, data []byte, create bool) ([]resource.Field, error) {
	fields = resource.CloneFields(fields)

	var obj map[string]any
	if err := yaml.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal edited data: %w", err)
	}

	// Process all fields that have a patch template.
	// For each field, convert the YAML value and compare with the original value.
	for key, field := range resource.IterFields(fields) {
		var patch *resource.Patch
		if create {
			patch = field.Create
		} else {
			patch = field.Edit
		}
		if patch == nil || patch.Set == nil {
			continue
		}

		// Visual editing only supports Set operations - always clear Add/Del
		patch.Add = nil
		patch.Del = nil

		// Get the value from the edited YAML
		newValue, found := getNestedValue(obj, key)

		// Mark this path as consumed so we can detect unknown fields
		deleteNestedValue(obj, key)

		if !found {
			// Field not in YAML
			if create {
				// Create mode: preserve non-zero defaults
				if value.IsZero(patch.Set) {
					patch.Set = nil
				}
			} else {
				// Edit mode: user explicitly removed the field, clear the patch
				patch.Set = nil
			}
			continue
		}

		// Convert newValue to the correct type (using patch.Set as type template)
		convertedValue, err := convertValue(newValue, reflect.TypeOf(patch.Set))
		if err != nil {
			return nil, fmt.Errorf("failed to convert value for field %s: %w", key, err)
		}

		if originalValue := field.Value; originalValue != nil {
			convertedOriginal, err := convertValue(originalValue, reflect.TypeOf(patch.Set))
			if err == nil && reflect.DeepEqual(convertedOriginal, convertedValue) {
				if !create {
					// Edit mode: unchanged value means no patch
					patch.Set = nil
					continue
				}

				// Create mode: only preserve patch if the unchanged value is non-zero.
				// If the value is still the type's zero value (e.g. empty string),
				// clear the patch so required validation still treats it as missing.
				if value.IsZero(convertedValue) {
					patch.Set = nil
					continue
				}
			}
		}

		// Value differs from original (or field wasn't displayed) - keep the patch
		patch.Set = convertedValue
	}

	// Check for unknown fields
	unknownFields := collectKeys(obj, nil)
	if len(unknownFields) > 0 {
		return nil, fmt.Errorf("unknown fields: %v", unknownFields)
	}

	// Return only fields with pending patches
	if create {
		return FilterCreateFields(fields), nil
	}
	return FilterEditFields(fields), nil
}

// getNestedValue retrieves a value from a nested map structure based on a field path.
func getNestedValue(m map[string]any, path resource.FieldPath) (any, bool) {
	if len(path) == 0 {
		return nil, false
	}
	value, ok := m[path[0]]
	if !ok {
		return nil, false
	}
	if len(path) == 1 {
		return value, true
	}
	nested, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	return getNestedValue(nested, path[1:])
}

// setNestedValue sets a value in a nested map structure based on a field path.
func setNestedValue(m map[string]any, path resource.FieldPath, value any) {
	if len(path) == 0 {
		return
	}
	if len(path) == 1 {
		m[path[0]] = value
		return
	}
	// Create nested map if needed
	key := path[0]
	if _, ok := m[key]; !ok {
		m[key] = make(map[string]any)
	}
	if nested, ok := m[key].(map[string]any); ok {
		setNestedValue(nested, path[1:], value)
	}
}

// deleteNestedValue removes a value from a nested map structure and cleans up empty parents.
func deleteNestedValue(m map[string]any, path resource.FieldPath) {
	if len(path) == 0 {
		return
	}
	if len(path) == 1 {
		delete(m, path[0])
		return
	}
	value, ok := m[path[0]]
	if !ok {
		return
	}
	nested, ok := value.(map[string]any)
	if !ok {
		return
	}
	deleteNestedValue(nested, path[1:])
	// Clean up empty nested maps
	if len(nested) == 0 {
		delete(m, path[0])
	}
}

// collectKeys collects all remaining keys in a nested map as field paths.
func collectKeys(m map[string]any, prefix resource.FieldPath) []string {
	var keys []string
	for k, v := range m {
		path := append(prefix, k)
		if nested, ok := v.(map[string]any); ok {
			keys = append(keys, collectKeys(nested, path)...)
		} else {
			keys = append(keys, path.String())
		}
	}
	return keys
}

// convertValue converts a value to the target type by round-tripping through YAML.
// This ensures proper handling of all YAML-supported types.
func convertValue(value any, targetType reflect.Type) (any, error) {
	if value == nil {
		return reflect.Zero(targetType).Interface(), nil
	}

	valueType := reflect.TypeOf(value)
	if valueType.AssignableTo(targetType) {
		return value, nil
	}
	if valueType.ConvertibleTo(targetType) {
		return reflect.ValueOf(value).Convert(targetType).Interface(), nil
	}

	// Round-trip through YAML for complex type conversions
	yamlBytes, err := yaml.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %T to %s: failed to marshal: %w", value, targetType.String(), err)
	}

	target := reflect.New(targetType).Interface()
	if err := yaml.Unmarshal(yamlBytes, target); err != nil {
		return nil, fmt.Errorf("cannot convert %T to %s: failed to unmarshal: %w", value, targetType.String(), err)
	}
	return reflect.ValueOf(target).Elem().Interface(), nil
}

// normalizeValue converts nil slices/maps to empty ones for cleaner YAML output.
// This ensures `services: []` instead of `services: null`.
func normalizeValue(v any) any {
	if v == nil {
		return v
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Map:
		if rv.IsNil() {
			return reflect.MakeSlice(rv.Type(), 0, 0).Interface()
		}
	}
	return v
}

func getEditor() (string, error) {
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor, nil
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor, nil
	}
	return "", fmt.Errorf("no editor set: please set $VISUAL or $EDITOR")
}
