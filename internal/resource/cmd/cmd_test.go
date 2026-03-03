// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lunixbochs/vtclean"
	"github.com/mitchellh/copystructure"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	resourcet "unikraft.com/cli/internal/resource/testing"
	xkong "unikraft.com/cli/internal/x/kong"
)

var baseTestStore = map[string]resourcet.TestResource{
	"test1": {
		ID:        "id-test1",
		Name:      "test1",
		State:     "pending",
		URL:       "https://example.com",
		Hidden:    "hidden-test1",
		Invisible: "invisible-test1",
		Settings: resourcet.TestSettings{
			X: 42,
			Y: "hello",
		},
		Authors: []resourcet.TestAuthor{
			{Name: "Alice", Email: "alice@example.com"},
			{Name: "Bob", Email: "bob@example.com"},
		},
	},
	"test2": {
		ID:        "id-test2",
		Name:      "test2",
		State:     "pending",
		URL:       "https://example.org",
		Hidden:    "hidden-test2",
		Invisible: "invisible-test2",
		Settings: resourcet.TestSettings{
			X: 7,
			Y: "world",
		},
		Authors: []resourcet.TestAuthor{
			{Name: "Charlie", Email: "charlie@example.com"},
			{Name: "Dana", Email: "dana@example.com"},
		},
	},
}

func TestList(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}

	cloned, err := copystructure.Copy(baseTestStore)
	require.NoError(t, err)
	resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

	var empty resourcet.TestResource
	resources, err := empty.List(ctx)
	require.NoError(t, err)
	assert.Len(t, resources, 2)

	var listOut bytes.Buffer
	listCmd := &ResourceListCmd[resourcet.TestResource]{}
	err = listCmd.Run(ctx, testStdio(&listOut), sandbox)
	require.NoError(t, err)

	output := listOut.String()
	assert.Contains(t, output, "test1")
	assert.Contains(t, output, "test2")
	assert.Contains(t, output, "id-test1")
	assert.Contains(t, output, "id-test2")

	t.Run("field", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			FormatOpts: FormatOpts{
				Field: xkong.HyphenStrings{"name", "id"},
			},
		}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "id-test1")
		assert.NotContains(t, output, "https://example.com")

		out.Reset()
		cmd.Name = []string{"test1"}
		err = cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		output = out.String()
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "id-test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("field exclude", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			FormatOpts: FormatOpts{
				Field:  xkong.HyphenStrings{"-url"},
				Output: Printer{Type: PrinterTypeKeyValue},
			},
		}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "name:")
		assert.Contains(t, output, "id:")
		assert.NotContains(t, output, "url:")
	})

	t.Run("filter", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Filter: []string{"name==test1"},
		}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")

		out.Reset()
		cmd.Name = []string{"test1", "test2"}
		err = cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		output = out.String()
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})
}

func TestListOutput(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}

	cloned, err := copystructure.Copy(baseTestStore)
	require.NoError(t, err)
	resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

	runList := func(t *testing.T, opts FormatOpts) string {
		t.Helper()
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			FormatOpts: opts,
		}
		err = cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)
		return out.String()
	}

	t.Run("table", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeTable}})
		cleaned := vtclean.Clean(output, false)
		assert.Contains(t, cleaned, "test1")
		assert.Contains(t, cleaned, "test2")
		assert.Contains(t, cleaned, "id-test1")
	})

	t.Run("kv", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeKeyValue}})
		assert.Contains(t, output, "name:")
		assert.Contains(t, output, "id:")
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "id-test1")
		assert.Contains(t, output, "test2")
	})

	t.Run("json", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeJSON}})
		var resources []map[string]any
		err := json.Unmarshal([]byte(output), &resources)
		require.NoError(t, err)
		require.Len(t, resources, 2)

		names := map[string]bool{}
		for _, res := range resources {
			if name, ok := res["name"].(string); ok {
				names[name] = true
			}
		}
		assert.True(t, names["test1"])
		assert.True(t, names["test2"])
	})

	t.Run("yaml", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeYAML}})
		var resources []map[string]any
		err := yaml.Unmarshal([]byte(output), &resources)
		require.NoError(t, err)
		require.Len(t, resources, 2)

		names := map[string]bool{}
		for _, res := range resources {
			if name, ok := res["name"].(string); ok {
				names[name] = true
			}
		}
		assert.True(t, names["test1"])
		assert.True(t, names["test2"])
	})

	t.Run("raw", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeRaw}})
		var resources []resourcet.TestResource
		err := json.Unmarshal([]byte(output), &resources)
		require.NoError(t, err)
		require.Len(t, resources, 2)

		names := map[string]bool{}
		for _, res := range resources {
			names[res.Name] = true
		}
		assert.True(t, names["test1"])
		assert.True(t, names["test2"])
	})

	t.Run("quiet", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeQuiet}})
		assert.Equal(t, "test1\ntest2\n", output)
	})

	t.Run("quiet field", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeQuiet}, Field: xkong.HyphenStrings{"id", "url"}})
		assert.Equal(t, "id-test1 https://example.com\nid-test2 https://example.org\n", output)
	})

	t.Run("template", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeTemplate, Value: "{{.name}}-{{.id}}"}})
		assert.Equal(t, "test1-id-test1\ntest2-id-test2\n", output)
	})
}

func TestTableNestedFieldSelection(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}

	cloned, err := copystructure.Copy(baseTestStore)
	require.NoError(t, err)
	resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

	var out bytes.Buffer
	cmd := &ResourceGetCmd[resourcet.TestResource]{
		Name: []string{"test1"},
		FormatOpts: FormatOpts{
			Output: Printer{Type: PrinterTypeTable},
			Field:  xkong.HyphenStrings{"name", "authors"},
		},
	}
	err = cmd.Run(ctx, testStdio(&out), sandbox)
	require.NoError(t, err)

	cleaned := vtclean.Clean(out.String(), false)
	assert.Contains(t, cleaned, "Alice")
	assert.Contains(t, cleaned, "alice@example.com")
}

func TestGet(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}

	cloned, err := copystructure.Copy(baseTestStore)
	require.NoError(t, err)
	resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

	var empty resourcet.TestResource
	resources, err := empty.Get(ctx, []string{"test1"})
	require.NoError(t, err)
	require.Len(t, resources, 1)

	test := resources[0].(resourcet.TestResource)
	assert.Equal(t, "test1", test.Name)
	assert.Equal(t, "id-test1", test.ID)
	assert.Equal(t, 42, test.Settings.X)
	assert.Equal(t, "hello", test.Settings.Y)

	fields, err := test.Fields()
	require.NoError(t, err)
	assert.NotEmpty(t, fields)

	var inspectOut bytes.Buffer
	inspectCmd := &ResourceGetCmd[resourcet.TestResource]{
		Name: []string{"test1"},
	}
	err = inspectCmd.Run(ctx, testStdio(&inspectOut), sandbox)
	require.NoError(t, err)

	output := inspectOut.String()
	assert.Contains(t, output, "test1")
	assert.Contains(t, output, "id-test1")
	assert.Contains(t, output, "https://example.com")

	t.Run("no_args", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceGetCmd[resourcet.TestResource]{
			Name: []string{},
		}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.Error(t, err)
	})

	t.Run("multiple", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceGetCmd[resourcet.TestResource]{
			Name: []string{"test1", "test2"},
		}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "test2")
		assert.Contains(t, output, "id-test1")
		assert.Contains(t, output, "id-test2")
	})

	t.Run("field", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceGetCmd[resourcet.TestResource]{
			Name: []string{"test1"},
			FormatOpts: FormatOpts{
				Field: xkong.HyphenStrings{"id", "url"},
			},
		}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "id-test1")
		assert.Contains(t, output, "https://example.com")

		out.Reset()
		cmd.Name = []string{"test1", "test2"}
		err = cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		output = out.String()
		assert.Contains(t, output, "id-test1")
		assert.Contains(t, output, "https://example.com")
		assert.Contains(t, output, "id-test2")
		assert.Contains(t, output, "https://example.org")
	})
}

func TestFieldVerbosity(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}

	cloned, err := copystructure.Copy(baseTestStore)
	require.NoError(t, err)
	resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

	runList := func(t *testing.T, fields []string) string {
		t.Helper()
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			FormatOpts: FormatOpts{
				Field: fields,
			},
		}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)
		return out.String()
	}

	runInspect := func(t *testing.T, fields []string) string {
		t.Helper()
		var out bytes.Buffer
		cmd := &ResourceGetCmd[resourcet.TestResource]{
			Name: []string{"test1"},
			FormatOpts: FormatOpts{
				Field: fields,
			},
		}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)
		return out.String()
	}

	t.Run("list_short_fields", func(t *testing.T) {
		output := vtclean.Clean(runList(t, nil), false)
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "id-test1")
		assert.NotContains(t, output, "hello")
		assert.NotContains(t, output, "hidden-test1")
		assert.NotContains(t, output, "invisible-test1")
	})

	t.Run("inspect_short_long_fields", func(t *testing.T) {
		output := runInspect(t, nil)
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "id-test1")
		assert.Contains(t, output, "hello")
		assert.NotContains(t, output, "hidden-test1")
		assert.NotContains(t, output, "invisible-test1")
	})

	t.Run("inspect_hidden_fields", func(t *testing.T) {
		output := runInspect(t, []string{"hidden"})
		assert.Contains(t, output, "hidden-test1")
		assert.NotContains(t, output, "invisible-test1")
	})

	t.Run("inspect_all_fields", func(t *testing.T) {
		output := runInspect(t, []string{"all"})
		assert.Contains(t, output, "id-test1")
		assert.Contains(t, output, "hello")
		assert.Contains(t, output, "hidden-test1")
		assert.NotContains(t, output, "invisible-test1")
	})

	t.Run("inspect_invisible_fields", func(t *testing.T) {
		output := runInspect(t, []string{"invisible"})
		assert.NotContains(t, output, "invisible-test1")
	})
}

func TestGetOutput(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}

	cloned, err := copystructure.Copy(baseTestStore)
	require.NoError(t, err)
	resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

	runInspect := func(t *testing.T, printer Printer) string {
		t.Helper()
		var out bytes.Buffer
		cmd := &ResourceGetCmd[resourcet.TestResource]{
			Name: []string{"test1", "test2"},
			FormatOpts: FormatOpts{
				Output: printer,
			},
		}
		err = cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)
		return out.String()
	}

	t.Run("quiet", func(t *testing.T) {
		output := runInspect(t, Printer{Type: PrinterTypeQuiet})
		assert.Equal(t, "test1\ntest2\n", output)
	})

	// Other formats are covered in TestListOutput.
}

func TestWait(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}

	t.Run("already_matching", func(t *testing.T) {
		cloned, err := copystructure.Copy(baseTestStore)
		require.NoError(t, err)
		resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

		resourceOne := resourcet.TestStore["test1"]
		resourceOne.State = "ready"
		resourcet.TestStore["test1"] = resourceOne

		resourceTwo := resourcet.TestStore["test2"]
		resourceTwo.State = "ready"
		resourcet.TestStore["test2"] = resourceTwo

		cmd := &ResourceWaitCmd[resourcet.TestResource]{
			Name:     []string{"test1", "test2"},
			Until:    []string{"state==ready"},
			Timeout:  time.Second,
			Interval: 10 * time.Millisecond,
		}
		err = cmd.Run(ctx, testStdio(&bytes.Buffer{}), sandbox)
		require.NoError(t, err)
	})

	t.Run("timeout", func(t *testing.T) {
		cloned, err := copystructure.Copy(baseTestStore)
		require.NoError(t, err)
		resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

		cmd := &ResourceWaitCmd[resourcet.TestResource]{
			Name:     []string{"test1", "test2"},
			Until:    []string{"state==ready"},
			Timeout:  1 * time.Second,
			Interval: 10 * time.Millisecond,
		}
		err = cmd.Run(ctx, testStdio(&bytes.Buffer{}), sandbox)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

func TestWaitOutput(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}

	cloned, err := copystructure.Copy(baseTestStore)
	require.NoError(t, err)
	resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

	for key, res := range resourcet.TestStore {
		res.State = "ready"
		resourcet.TestStore[key] = res
	}

	runWait := func(t *testing.T, printer Printer) string {
		t.Helper()
		var out bytes.Buffer
		cmd := &ResourceWaitCmd[resourcet.TestResource]{
			Name:     []string{"test1", "test2"},
			Until:    []string{"state==ready"},
			Interval: 10 * time.Millisecond,
			FormatOpts: FormatOpts{
				Output: printer,
			},
		}
		err = cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)
		return out.String()
	}

	t.Run("quiet", func(t *testing.T) {
		output := runWait(t, Printer{Type: PrinterTypeQuiet})
		assert.Equal(t, "test1\ntest2\n", output)
	})

	// Other formats are covered in TestListOutput.
}

func TestCreate(t *testing.T) {
	ctx := context.Background()

	resourcet.TestStore = map[string]resourcet.TestResource{}

	var empty resourcet.TestResource
	templateFields, err := empty.Fields()
	require.NoError(t, err)

	for key, field := range resource.IterFields(templateFields) {
		if field.Create == nil {
			continue
		}
		switch key.String() {
		case "name":
			field.Create.Set = "test-new"
		case "settings.x":
			field.Create.Set = 100
		case "settings.y":
			field.Create.Set = "created"
		}
	}

	res, err := empty.Create(ctx, templateFields)
	require.NoError(t, err)
	require.Len(t, res, 1)

	created := res[0].(resourcet.TestResource)
	assert.Equal(t, "test-new", created.Name)
	assert.Equal(t, 100, created.Settings.X)
	assert.Equal(t, "created", created.Settings.Y)
	assert.Contains(t, resourcet.TestStore, "test-new")
}

func TestCreateOutput(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}
	resourcet.TestStore = map[string]resourcet.TestResource{}

	runCreate := func(t *testing.T, printer Printer) string {
		t.Helper()
		var out bytes.Buffer
		cmd := &ResourceCreateCmd[resourcet.TestResource]{
			SetArgs: SetArgs{
				Set: []map[string]string{
					{"name": "test-output"},
					{"settings.x": "100"},
					{"settings.y": "created"},
				},
			},
			FormatOpts: FormatOpts{
				Output: printer,
			},
		}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)
		return out.String()
	}

	t.Run("quiet", func(t *testing.T) {
		output := runCreate(t, Printer{Type: PrinterTypeQuiet})
		assert.Equal(t, "test-output\n", output)
	})

	// Other formats are covered in TestListOutput.
}

func TestCreateDryRun(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}
	resourcet.TestStore = map[string]resourcet.TestResource{}

	var out bytes.Buffer
	cmd := &ResourceCreateCmd[resourcet.TestResource]{
		DryRun: true,
		SetArgs: SetArgs{
			Set: []map[string]string{
				{"name": "test-dry"},
				{"settings.x": "100"},
				{"settings.y": "created"},
			},
		},
	}
	err := cmd.Run(ctx, testStdio(&out), sandbox)
	require.NoError(t, err)

	assert.NotContains(t, resourcet.TestStore, "test-dry")

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 3)

	expected := [][]string{
		{"name", ":=", "test-dry"},
		{"settings.x", ":=", "100"},
		{"settings.y", ":=", "created"},
	}
	for i, expectedFields := range expected {
		assert.Equal(t, expectedFields, strings.Fields(lines[i]))
	}
}

func TestCreatePatchSpecFileArgs(t *testing.T) {
	nameFile := tempFile(t, " test-file ")
	setFile := tempFile(t, " 123 \n")
	setTextFile := tempFile(t, " created\n")

	cmd := &ResourceCreateCmd[resourcet.TestResource]{
		SetArgs: SetArgs{
			Set: []map[string]string{{"name": "test-inline"}},
			SetFile: []map[string]string{
				{"name": nameFile},
				{"settings.x": setFile},
				{"settings.y": setTextFile},
			},
		},
	}

	spec, err := cmd.toPatchSpec()
	require.NoError(t, err)

	assert.Equal(t, []string{"test-inline", "test-file"}, spec.Set["name"])
	assert.Equal(t, []string{"123"}, spec.Set["settings.x"])
	assert.Equal(t, []string{"created"}, spec.Set["settings.y"])
}

func TestCreateSetFile(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}
	resourcet.TestStore = map[string]resourcet.TestResource{}

	nameFile := tempFile(t, " test-file ")
	setFile := tempFile(t, " 101 \n")
	setTextFile := tempFile(t, " created\n")

	var out bytes.Buffer
	cmd := &ResourceCreateCmd[resourcet.TestResource]{
		SetArgs: SetArgs{
			SetFile: []map[string]string{
				{"name": nameFile},
				{"settings.x": setFile},
				{"settings.y": setTextFile},
			},
		},
	}

	err := cmd.Run(ctx, testStdio(&out), sandbox)
	require.NoError(t, err)

	created, ok := resourcet.TestStore["test-file"]
	require.True(t, ok)
	assert.Equal(t, 101, created.Settings.X)
	assert.Equal(t, "created", created.Settings.Y)
}

func TestEdit(t *testing.T) {
	ctx := context.Background()

	editStore := map[string]resourcet.TestResource{
		"test-edit": {
			ID:   "id-edit",
			Name: "test-edit",
			URL:  "https://example.com",
			Settings: resourcet.TestSettings{
				X: 10,
				Y: "original",
			},
		},
	}
	cloned, err := copystructure.Copy(editStore)
	require.NoError(t, err)
	resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

	var empty resourcet.TestResource
	resources, err := empty.Get(ctx, []string{"test-edit"})
	require.NoError(t, err)
	require.Len(t, resources, 1)

	target := resources[0]
	templateFields, err := target.Fields()
	require.NoError(t, err)

	for key, field := range resource.IterFields(templateFields) {
		if field.Edit == nil {
			continue
		}
		switch key.String() {
		case "settings.x":
			field.Edit.Set = 999
		case "settings.y":
			field.Edit.Set = "modified"
		}
	}

	res, err := empty.Edit(ctx, target, templateFields)
	require.NoError(t, err)

	edited := res.(resourcet.TestResource)
	assert.Equal(t, "test-edit", edited.Name)
	assert.Equal(t, "id-edit", edited.ID)
	assert.Equal(t, 999, edited.Settings.X)
	assert.Equal(t, "modified", edited.Settings.Y)

	stored := resourcet.TestStore["test-edit"]
	assert.Equal(t, 999, stored.Settings.X)
	assert.Equal(t, "modified", stored.Settings.Y)
}

func TestEditOutput(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}

	editStore := map[string]resourcet.TestResource{
		"test-edit": {
			ID:   "id-edit",
			Name: "test-edit",
			URL:  "https://example.com",
			Settings: resourcet.TestSettings{
				X: 10,
				Y: "original",
			},
		},
	}
	cloned, err := copystructure.Copy(editStore)
	require.NoError(t, err)
	resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

	runEdit := func(t *testing.T, printer Printer) string {
		t.Helper()
		var out bytes.Buffer
		cmd := &ResourceEditCmd[resourcet.TestResource]{
			Name: "test-edit",
			SetArgs: SetArgs{
				Set: []map[string]string{
					{"settings.x": "999"},
				},
			},
			FormatOpts: FormatOpts{
				Output: printer,
			},
		}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)
		return out.String()
	}

	t.Run("quiet", func(t *testing.T) {
		output := runEdit(t, Printer{Type: PrinterTypeQuiet})
		assert.Equal(t, "test-edit\n", output)
	})

	// Other formats are covered in TestListOutput.
}

func TestEditDryRun(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}

	editStore := map[string]resourcet.TestResource{
		"test-edit": {
			ID:   "id-edit",
			Name: "test-edit",
			URL:  "https://example.com",
			Settings: resourcet.TestSettings{
				X: 10,
				Y: "original",
			},
		},
	}
	cloned, err := copystructure.Copy(editStore)
	require.NoError(t, err)
	resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

	var out bytes.Buffer
	cmd := &ResourceEditCmd[resourcet.TestResource]{
		Name:   "test-edit",
		DryRun: true,
		SetArgs: SetArgs{
			Set: []map[string]string{
				{"settings.x": "999"},
				{"settings.y": "modified"},
			},
		},
	}
	err = cmd.Run(ctx, testStdio(&out), sandbox)
	require.NoError(t, err)

	stored := resourcet.TestStore["test-edit"]
	assert.Equal(t, 10, stored.Settings.X)
	assert.Equal(t, "original", stored.Settings.Y)

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 2)

	expected := [][]string{
		{"settings.x", ":=", "999"},
		{"settings.y", ":=", "modified"},
	}
	for i, expectedFields := range expected {
		assert.Equal(t, expectedFields, strings.Fields(lines[i]))
	}
}

func TestEditPatchSpecFileArgs(t *testing.T) {
	setFile := tempFile(t, " 123 \n")
	addFile := tempFile(t, " new-entry\n")
	delFile := tempFile(t, " old-entry\n")

	cmd := &ResourceEditCmd[resourcet.TestResource]{
		SetArgs: SetArgs{
			Set:     []map[string]string{{"settings.y": "inline"}},
			SetFile: []map[string]string{{"settings.x": setFile}},
		},
		AddArgs: AddArgs{
			Add:     []map[string]string{{"authors": "inline-entry"}},
			AddFile: []map[string]string{{"authors": addFile}},
		},
		DelArgs: DelArgs{
			Del:     []map[string]string{{"url": "inline-entry"}},
			DelFile: []map[string]string{{"url": delFile}},
		},
	}

	spec, err := cmd.toPatchSpec()
	require.NoError(t, err)

	assert.Equal(t, []string{"inline"}, spec.Set["settings.y"])
	assert.Equal(t, []string{"123"}, spec.Set["settings.x"])
	assert.Equal(t, []string{"inline-entry", "new-entry"}, spec.Add["authors"])
	assert.Equal(t, []string{"inline-entry", "old-entry"}, spec.Del["url"])
}

func TestDelete(t *testing.T) {
	ctx := context.Background()

	deleteStore := map[string]resourcet.TestResource{
		"test-delete": {
			ID:   "id-delete",
			Name: "test-delete",
			URL:  "https://example.com",
		},
		"test-keep": {
			ID:   "id-keep",
			Name: "test-keep",
			URL:  "https://example.org",
		},
	}
	cloned, err := copystructure.Copy(deleteStore)
	require.NoError(t, err)
	resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

	var empty resourcet.TestResource
	resources, err := empty.Get(ctx, []string{"test-delete"})
	require.NoError(t, err)
	require.Len(t, resources, 1)

	err = empty.Delete(ctx, resources)
	require.NoError(t, err)

	assert.NotContains(t, resourcet.TestStore, "test-delete")
	assert.Contains(t, resourcet.TestStore, "test-keep")

	resources, err = empty.Get(ctx, []string{"test-delete"})
	require.NoError(t, err)
	assert.Empty(t, resources)
}

func TestRemoveOutput(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}

	t.Run("no_args", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceRemoveCmd[resourcet.TestResource]{}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.Error(t, err)
	})

	runRemove := func(t *testing.T, printer Printer) string {
		t.Helper()
		cloned, err := copystructure.Copy(baseTestStore)
		require.NoError(t, err)
		resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

		var out bytes.Buffer
		cmd := &ResourceRemoveCmd[resourcet.TestResource]{
			Name: []string{"test1", "test2"},
			FormatOpts: FormatOpts{
				Output: printer,
			},
		}
		err = cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)
		return out.String()
	}

	t.Run("quiet", func(t *testing.T) {
		output := runRemove(t, Printer{Type: PrinterTypeQuiet})
		assert.Equal(t, "test1\ntest2\n", output)
	})

	t.Run("kv", func(t *testing.T) {
		output := runRemove(t, Printer{Type: PrinterTypeKeyValue})
		assert.Contains(t, output, "name:")
		assert.Contains(t, output, "id:")
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "id-test1")
		assert.Contains(t, output, "test2")
	})

	// Other formats are covered in TestListOutput.
}

func tempFile(t *testing.T, contents string) string {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "set-file-*")
	require.NoError(t, err)

	_, err = file.WriteString(contents)
	require.NoError(t, err)

	_, err = file.Seek(0, io.SeekStart)
	require.NoError(t, err)

	return file.Name()
}

func testStdio(out io.Writer) config.Stdio {
	return config.Stdio{
		Stdin:  &bytes.Buffer{},
		Stdout: out,
		Stderr: out,
	}
}

func testStdioWithInput(out io.Writer, in io.Reader) config.Stdio {
	return config.Stdio{
		Stdin:  in,
		Stdout: out,
		Stderr: out,
	}
}

func TestValueCallback(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}

	// Reset state - use TestStore with TestResource
	resourcet.TestStore = map[string]resourcet.TestResource{
		"res1": {ID: "1", Name: "res1", State: "running"},
		"res2": {ID: "2", Name: "res2", State: "stopped"},
	}
	resourcet.CallbackInvocations = 0

	t.Run("list_without_lazy_field", func(t *testing.T) {
		resourcet.CallbackInvocations = 0

		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "res1")
		assert.Contains(t, output, "running")
		assert.NotContains(t, output, "computed-")
		// Callback should not be invoked when lazy field is not requested
		assert.Equal(t, 0, resourcet.CallbackInvocations, "callbacks should not be invoked when lazy field not selected")
	})

	t.Run("list_with_lazy_field", func(t *testing.T) {
		resourcet.CallbackInvocations = 0

		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			FormatOpts: FormatOpts{
				Field: xkong.HyphenStrings{"+lazy"},
			},
		}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "res1")
		assert.Contains(t, output, "computed-res1")
		assert.Contains(t, output, "res2")
		assert.Contains(t, output, "computed-res2")
		// Callback should be invoked once per resource
		assert.Equal(t, 2, resourcet.CallbackInvocations, "callbacks should be invoked for each resource")
	})

	t.Run("get_with_lazy_field", func(t *testing.T) {
		resourcet.CallbackInvocations = 0

		var out bytes.Buffer
		cmd := &ResourceGetCmd[resourcet.TestResource]{
			Name: []string{"res1"},
			FormatOpts: FormatOpts{
				Field: xkong.HyphenStrings{"+lazy"},
			},
		}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "res1")
		assert.Contains(t, output, "computed-res1")
		assert.Equal(t, 1, resourcet.CallbackInvocations, "callback should be invoked once")
	})

	t.Run("quiet_output_with_lazy_field", func(t *testing.T) {
		resourcet.CallbackInvocations = 0

		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			FormatOpts: FormatOpts{
				Output: Printer{Type: PrinterTypeQuiet},
				Field:  xkong.HyphenStrings{"name", "lazy"},
			},
		}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "res1 computed-res1")
		assert.Contains(t, output, "res2 computed-res2")
		assert.Equal(t, 2, resourcet.CallbackInvocations)
	})

	t.Run("filter_on_lazy_field_without_selecting_it", func(t *testing.T) {
		resourcet.CallbackInvocations = 0

		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			// Filter on lazy field, but don't select it for output
			Filter: []string{"lazy==computed-res1"},
		}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		output := out.String()
		// Should only show res1 (filtered by lazy field)
		assert.Contains(t, output, "res1")
		assert.NotContains(t, output, "res2")
		// Callbacks should be invoked to evaluate the filter
		assert.Equal(t, 2, resourcet.CallbackInvocations, "callbacks should be invoked to evaluate filter")
	})
}

func TestDeleteBulk(t *testing.T) {
	ctx := context.Background()
	sandbox := &resource.Sandbox{}

	t.Run("all_with_confirmation", func(t *testing.T) {
		cloned, err := copystructure.Copy(baseTestStore)
		require.NoError(t, err)
		resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

		var out bytes.Buffer
		in := strings.NewReader("YES\n")
		cmd := &ResourceBulkRemoveCmd[resourcet.TestResource]{
			All: true,
		}
		err = cmd.Run(ctx, testStdioWithInput(&out, in), sandbox)
		require.NoError(t, err)

		// All resources should be deleted
		assert.Empty(t, resourcet.TestStore)

		output := out.String()
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "test2")
	})

	t.Run("all_with_force", func(t *testing.T) {
		cloned, err := copystructure.Copy(baseTestStore)
		require.NoError(t, err)
		resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

		var out bytes.Buffer
		cmd := &ResourceBulkRemoveCmd[resourcet.TestResource]{
			All:   true,
			Force: true,
		}
		err = cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		// All resources should be deleted
		assert.Empty(t, resourcet.TestStore)
	})

	t.Run("all_cancelled", func(t *testing.T) {
		cloned, err := copystructure.Copy(baseTestStore)
		require.NoError(t, err)
		resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

		var out bytes.Buffer
		in := strings.NewReader("no\n")
		cmd := &ResourceBulkRemoveCmd[resourcet.TestResource]{
			All: true,
		}
		err = cmd.Run(ctx, testStdioWithInput(&out, in), sandbox)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cancelled")

		// Resources should not be deleted
		assert.Len(t, resourcet.TestStore, 2)
	})

	t.Run("empty", func(t *testing.T) {
		resourcet.TestStore = map[string]resourcet.TestResource{}

		var out bytes.Buffer
		cmd := &ResourceBulkRemoveCmd[resourcet.TestResource]{
			All:   true,
			Force: true,
		}
		err := cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		output := out.String()
		assert.NotContains(t, output, "test1")
	})

	t.Run("filter_with_force", func(t *testing.T) {
		cloned, err := copystructure.Copy(baseTestStore)
		require.NoError(t, err)
		resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

		// Change state of test1 to "running" so we can filter
		res := resourcet.TestStore["test1"]
		res.State = "running"
		resourcet.TestStore["test1"] = res

		var out bytes.Buffer
		cmd := &ResourceBulkRemoveCmd[resourcet.TestResource]{
			Filter: []string{"state==running"},
			Force:  true,
		}
		err = cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		// Only test1 should be deleted (matches filter)
		assert.NotContains(t, resourcet.TestStore, "test1")
		// test2 should still exist (doesn't match filter)
		assert.Contains(t, resourcet.TestStore, "test2")
	})

	t.Run("filter_no_match", func(t *testing.T) {
		cloned, err := copystructure.Copy(baseTestStore)
		require.NoError(t, err)
		resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

		var out bytes.Buffer
		cmd := &ResourceBulkRemoveCmd[resourcet.TestResource]{
			Filter: []string{"state==nonexistent"},
			Force:  true,
		}
		err = cmd.Run(ctx, testStdio(&out), sandbox)
		require.NoError(t, err)

		// No resources should be deleted (no matches)
		assert.Len(t, resourcet.TestStore, 2)
	})

	t.Run("filter_with_confirmation", func(t *testing.T) {
		cloned, err := copystructure.Copy(baseTestStore)
		require.NoError(t, err)
		resourcet.TestStore = cloned.(map[string]resourcet.TestResource)

		// Change state of test1 to "running" so we can filter
		res := resourcet.TestStore["test1"]
		res.State = "running"
		resourcet.TestStore["test1"] = res

		var out bytes.Buffer
		in := strings.NewReader("YES\n")
		cmd := &ResourceBulkRemoveCmd[resourcet.TestResource]{
			Filter: []string{"state==running"},
		}
		err = cmd.Run(ctx, testStdioWithInput(&out, in), sandbox)
		require.NoError(t, err)

		// Only test1 should be deleted
		assert.NotContains(t, resourcet.TestStore, "test1")
		assert.Contains(t, resourcet.TestStore, "test2")
	})
}
