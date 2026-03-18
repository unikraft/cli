// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/containerd/containerd/v2/pkg/filters"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/patch"
	"unikraft.com/cli/internal/tui/watcher"
	xfilters "unikraft.com/cli/internal/x/filters"
	xkong "unikraft.com/cli/internal/x/kong"
	"unikraft.com/cloud/sdk/platform/group"
)

type ResourceCmdInterface interface {
	Underlying() resource.Resource
}

type ResourceCmd[R resource.Resource] struct{}

var _ ResourceCmdInterface = (*ResourceCmd[resource.GettableResource])(nil)

func (cmd ResourceCmd[R]) Underlying() resource.Resource {
	var empty R
	return empty
}

type GettableResourceCmd[R resource.GettableResource] struct {
	Get ResourceGetCmd[R] `cmd:"" help:"Inspect a ${name}." aliases:"inspect,show"`
}
type WaitableResourceCmd[R resource.GettableResource] struct {
	Wait ResourceWaitCmd[R] `cmd:"" help:"Wait for ${names} to match a filter."`
}
type ListableResourceCmd[R resource.GettableListableResource] struct {
	List ResourceListCmd[R] `cmd:"" help:"List ${names}." aliases:"ls"`
}
type DeletableResourceCmd[R resource.DeletableResource] struct {
	Delete ResourceRemoveCmd[R] `cmd:"" help:"Remove a ${name}." aliases:"rm,remove"`
}

// BulkDeletableResourceCmd exposes a `delete` command which supports deleting
// resources by name as well as bulk deletion via `--all` and `--filter`.
//
// We keep this separate from DeletableResourceCmd so that resources which are
// deletable but not listable can still embed delete-by-name behavior.
type BulkDeletableResourceCmd[R interface {
	resource.DeletableResource
	resource.ListableResource
}] struct {
	Delete ResourceBulkRemoveCmd[R] `cmd:"" help:"Remove a ${name}." aliases:"rm,remove"`
}
type EditableResourceCmd[R resource.EditableResource] struct {
	Edit ResourceEditCmd[R] `cmd:"" help:"Edit a ${name}."`
}
type CreatableResourceCmd[R resource.CreatableResource] struct {
	Create ResourceCreateCmd[R] `cmd:"" help:"Create a ${name}."`
}

func (cmd ResourceCmd[R]) HelpSections() []kingkong.HelpSection {
	var r R

	fields, err := r.Fields()
	if err != nil {
		panic(err)
	}
	fields, err = selectResourceFields(fields, true, resource.FieldVerbosityInvisible, nil)
	if err != nil {
		panic(err)
	}

	buf := &bytes.Buffer{}

	for _, field := range fields {
		paths := []string{}
		base := kingkong.DimmedColor(field.Name)
		paths = append(paths, base)
		for path := range resource.IterFields(field.Subfields) {
			parts := make([]string, 0, len(path)+1)
			parts = append(parts, base)
			for _, part := range path {
				if part == "*" {
					part = kingkong.Bold(part)
				}
				parts = append(parts, kingkong.DimmedMoreColor(part))
			}
			paths = append(paths, strings.Join(parts, kingkong.DimmedColor(".")))
		}
		fmt.Fprintln(buf, strings.Join(paths, kingkong.DimmedColor(", ")))
	}

	return []kingkong.HelpSection{
		{
			Title:   "Fields",
			Content: buf.String(),
		},
	}
}

func (cmd ResourceCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeNone] // top-level
	}
	return nil
}

type FormatOpts struct {
	Field xkong.GreedyStrings `short:"f" help:"Specify which fields to include in the output."`

	Output Printer `short:"o" help:"Output format. One of: kv, table, json, yaml, raw, quiet, template."`
}

type ResourceListCmd[R resource.GettableListableResource] struct {
	Name   []string            `arg:"" optional:"" completion-predictor:"resource-key-${name}" help:"Names of the ${names} to list."`
	Filter []string            `help:"Filter output based on a field value (e.g. --filter state==running)." sep:"none"`
	Watch  *time.Duration      `short:"w" help:"Watch for changes and refresh output." type:"optional"`
	Sort   xkong.GreedyStrings `help:"Sort output by field values (e.g. --sort name,-timestamps.created-at). Use - prefix for descending, + for ascending."`

	FormatOpts

	// DefaultFilter is applied when listing.
	// HACK: not perfect, but lets other commands extend this one easily without
	// needing weirder runtime introspection
	DefaultFilter filters.Filter `kong:"-"`
}

func (cmd ResourceListCmd[R]) HelpSections() []kingkong.HelpSection {
	return ResourceCmd[R]{}.HelpSections()
}

func (cmd ResourceListCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeList]
	}
	return nil
}

func (cmd *ResourceListCmd[R]) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox) error {
	filter, err := filters.ParseAll(cmd.Filter...)
	if err != nil {
		return err
	}
	if cmd.DefaultFilter != nil {
		filter = filters.All{cmd.DefaultFilter, filter}
	}
	ctx = resource.WithFilter(ctx, filter)

	sortSpecs, err := parseSortSpecs(cmd.Sort...)
	if err != nil {
		return err
	}

	var empty R
	render := func(out io.Writer) error {
		var resources []resource.Resource
		var opErr error
		if len(cmd.Name) > 0 {
			r := sandbox.WrapGettable(empty)
			resources, opErr = r.Get(ctx, cmd.Name)
		} else {
			r := sandbox.WrapListable(empty)
			resources, opErr = r.List(ctx)
		}
		if opErr != nil && len(resources) == 0 {
			return opErr
		}

		resources, filterErr := filterResources(ctx, resources, filter)
		if filterErr != nil {
			opErr = errors.Join(opErr, filterErr)
		}

		if len(sortSpecs) > 0 {
			resources, err = sortResources(ctx, resources, sortSpecs)
			if err != nil {
				return errors.Join(opErr, err)
			}
		}

		printErr := cmd.Output.
			WithDefault(PrinterTypeTable).
			Print(ctx, out, cmd.Field, empty, resources...)
		if printErr != nil {
			return errors.Join(opErr, printErr)
		}
		return opErr
	}

	if cmd.Watch != nil {
		watch := cmp.Or(*cmd.Watch, 2*time.Second)
		return watcher.WatchOutput(ctx, watch, stdio.Stdout, render)
	}
	return render(stdio.Stdout)
}

type ResourceGetCmd[R resource.GettableResource] struct {
	Name  []string       `arg:"" completion-predictor:"resource-key-${name}" help:"Names of the ${names} to inspect."`
	Watch *time.Duration `short:"w" help:"Watch for changes and refresh output." type:"optional"`

	FormatOpts
}

func (cmd ResourceGetCmd[R]) HelpSections() []kingkong.HelpSection {
	return ResourceCmd[R]{}.HelpSections()
}

func (cmd ResourceGetCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeGet]
	}
	return nil
}

func (cmd *ResourceGetCmd[R]) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox) error {
	var empty R
	r := sandbox.WrapGettable(empty)

	render := func(out io.Writer) error {
		resources, opErr := r.Get(ctx, cmd.Name)
		if opErr != nil && len(resources) == 0 {
			return opErr
		}
		printErr := cmd.Output.
			WithDefault(PrinterTypeKeyValue).
			Print(ctx, out, cmd.Field, empty, resources...)
		if printErr != nil {
			return errors.Join(opErr, printErr)
		}
		return opErr
	}

	if cmd.Watch != nil {
		watch := cmp.Or(*cmd.Watch, 2*time.Second)
		return watcher.WatchOutput(ctx, watch, stdio.Stdout, render)
	}
	return render(stdio.Stdout)
}

type ResourceWaitCmd[R resource.GettableResource] struct {
	Name  []string `arg:"" completion-predictor:"resource-key-${name}" help:"Names of the ${names} to wait for."`
	Until []string `help:"Filter expression to wait for (e.g. --until state==running)." sep:"none" required:"" aliases:"filter"`

	Interval time.Duration `long:"interval" default:"2s" help:"Polling interval."`
	Timeout  time.Duration `long:"timeout" default:"0" help:"Timeout before giving up."`

	FormatOpts
}

func (cmd ResourceWaitCmd[R]) HelpSections() []kingkong.HelpSection {
	return ResourceCmd[R]{}.HelpSections()
}

func (cmd ResourceWaitCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeWait]
	}
	return nil
}

func (cmd *ResourceWaitCmd[R]) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox) error {
	var empty R
	if len(cmd.Name) == 0 {
		return fmt.Errorf("no %s specified", empty.Type().Names)
	}
	r := sandbox.WrapGettable(empty)

	filter, err := filters.ParseAll(cmd.Until...)
	if err != nil {
		return err
	}
	ctx = resource.WithFilter(ctx, filter)

	if cmd.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cmd.Timeout)
		defer cancel()
	}

	ticker := time.NewTicker(cmd.Interval)
	defer ticker.Stop()

	passing := map[string]bool{}
	for {
		resources, err := r.Get(ctx, cmd.Name)
		if err != nil {
			return err
		}

		filtered, err := filterResources(ctx, resources, filter)
		if err != nil {
			return err
		}
		if len(filtered) == len(resources) {
			log.G(ctx).Debug().
				Strs("resources", cmd.Name).
				Msg("all resources match the specified conditions")

			return cmd.Output.
				WithDefault(PrinterTypeKeyValue).
				Print(ctx, stdio.Stdout, []string(cmd.Field), empty, filtered...)
		}
		log.G(ctx).Debug().
			Strs("resources", cmd.Name).
			Int("matching", len(filtered)).
			Int("total", len(resources)).
			Msg("not all resources match the specified conditions yet")

		passed := passing
		passing = map[string]bool{}
		for _, res := range filtered {
			key := res.Key().String()
			passing[key] = true
			if ok := passed[key]; !ok {
				log.G(ctx).Info().Str("resource", key).
					Msg("resource now matches the specified conditions")
			}
		}
		for _, res := range resources {
			key := res.Key().String()
			if _, ok := passing[key]; ok {
				continue
			}
			passing[key] = false
			if ok := passed[key]; ok {
				log.G(ctx).Info().Str("resource", key).
					Msg("resource no longer matches the specified conditions")
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func filterResources(ctx context.Context, resources []resource.Resource, filter filters.Filter) (filtered []resource.Resource, rerr error) {
	if filter == nil {
		return resources, nil
	}

	// Extract field paths needed by the filter
	filterKeys := xfilters.Keys(filter)
	if len(filterKeys) == 0 {
		return resources, nil
	}
	filterPaths := make([]resource.FieldPath, len(filterKeys))
	for i, key := range filterKeys {
		filterPaths[i] = resource.FieldPath(key)
	}

	resolved, err := resolveResources(ctx, resources, filterPaths)
	if err != nil {
		return nil, err
	}

	for _, res := range resolved {
		fields, _ := res.Fields()
		if filter.Match(filters.AdapterFunc(func(key []string) (string, bool) {
			matched := resource.GetFieldByPath(fields, key)
			if matched == nil {
				return "", false
			}
			if len(matched) != 1 {
				// 0 fields = no exact match
				// >1 fields = ambiguous match
				return "", false
			}
			// HACK: strip escape sequences from rendered output
			out, _ := matched[0].Render()
			return ansi.Strip(out), true
		})) {
			filtered = append(filtered, res)
		}
	}
	return filtered, rerr
}

type ResourceRemoveCmd[R resource.DeletableResource] struct {
	Name []string `arg:"" completion-predictor:"resource-key-${name}" help:"Names of the ${names} to remove."`

	FormatOpts
}

func (cmd ResourceRemoveCmd[R]) HelpSections() []kingkong.HelpSection {
	return ResourceCmd[R]{}.HelpSections()
}

func (cmd ResourceRemoveCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeDelete]
	}
	return nil
}

func (cmd *ResourceRemoveCmd[R]) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox) error {
	var empty R
	r := sandbox.WrapDeletable(empty)
	resources, getErr := r.Get(ctx, cmd.Name)
	if getErr != nil && len(resources) == 0 {
		return getErr
	}

	deleteErr := error(nil)
	if len(resources) > 0 {
		deleteErr = r.Delete(ctx, resources)
	}

	toPrint := resources
	if deleteErr != nil {
		var notFound group.ErrRefNotFound
		if errors.As(deleteErr, &notFound) {
			missing := make(map[string]struct{}, len(notFound.Refs))
			for _, ref := range notFound.Refs {
				missing[multimetro.Key(ref).String()] = struct{}{}
			}
			toPrint = slices.DeleteFunc(slices.Clone(resources), func(r resource.Resource) bool {
				_, ok := missing[r.Key().String()]
				return ok
			})
		} else {
			// Unknown error shape: avoid claiming success.
			toPrint = nil
		}
	}

	printErr := cmd.Output.
		WithDefault(PrinterTypeQuiet).
		Print(ctx, stdio.Stdout, cmd.Field, empty, toPrint...)
	if printErr != nil {
		return errors.Join(getErr, deleteErr, printErr)
	}
	return errors.Join(getErr, deleteErr)
}

type ResourceBulkRemoveCmd[R interface {
	resource.DeletableResource
	resource.ListableResource
}] struct {
	Name []string `arg:"" optional:"" completion-predictor:"resource-key-${name}" help:"Names of the ${names} to remove."`

	All    bool     `xor:"select" help:"Remove all ${names}. Prompts for confirmation."`
	Filter []string `xor:"select" help:"Filter ${names} to remove (e.g. --filter state==running). Prompts for confirmation." sep:"none"`
	Force  bool     `help:"Do not prompt for confirmation when using --all or --filter."`

	FormatOpts
}

func (cmd ResourceBulkRemoveCmd[R]) HelpSections() []kingkong.HelpSection {
	return ResourceCmd[R]{}.HelpSections()
}

func (cmd ResourceBulkRemoveCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeDelete]
	}
	return nil
}

func (cmd *ResourceBulkRemoveCmd[R]) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox) error {
	var empty R
	var resources []resource.Resource
	if cmd.All || len(cmd.Filter) > 0 {
		if len(cmd.Name) > 0 {
			// would be nice if xor groups could enforce this
			return fmt.Errorf("cannot specify names when using --all or --filter")
		}

		filter, err := filters.ParseAll(cmd.Filter...)
		if err != nil {
			return err
		}

		r := sandbox.WrapListable(empty)
		resources, err = r.List(ctx)
		if err != nil {
			return err
		}

		if filter != nil {
			resources, err = filterResources(ctx, resources, filter)
			if err != nil {
				return err
			}
			resources = unwrapResources(resources)
		}

		if !cmd.Force && len(resources) > 0 {
			log.G(ctx).Warn().
				Int("count", len(resources)).
				Msg("resources will be deleted")

			err = cmd.Output.
				WithDefault(PrinterTypeTable).
				Print(ctx, stdio.Stdout, []string(cmd.Field), empty, resources...)
			if err != nil {
				return err
			}

			fmt.Fprintf(stdio.Stdout, "\nType \"yes\" to confirm deletion: ")

			inputCh := make(chan string, 1)
			errCh := make(chan error, 1)
			go func() {
				reader := bufio.NewReader(stdio.Stdin)
				response, err := reader.ReadString('\n')
				if err != nil && err != io.EOF {
					errCh <- fmt.Errorf("failed to read confirmation: %w", err)
					return
				}
				inputCh <- strings.TrimSpace(response)
			}()

			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-errCh:
				return err
			case response := <-inputCh:
				switch strings.ToLower(response) {
				case "y", "yes":
				default:
					return fmt.Errorf("deletion cancelled")
				}
			}
		}

		dr := sandbox.WrapDeletable(empty)
		err = dr.Delete(ctx, resources)
		if err != nil {
			return err
		}
		return nil
	} else if len(cmd.Name) > 0 {
		r := sandbox.WrapDeletable(empty)
		var err error
		resources, err = r.Get(ctx, cmd.Name)
		if err != nil {
			return err
		}

		err = r.Delete(ctx, resources)
		if err != nil {
			return err
		}
		return cmd.Output.
			WithDefault(PrinterTypeQuiet).
			Print(ctx, stdio.Stdout, []string(cmd.Field), empty, resources...)
	} else {
		return fmt.Errorf("no resources specified for deletion")
	}
}

type ResourceEditCmd[R resource.EditableResource] struct {
	Name string `arg:"" completion-predictor:"resource-key-${name}" help:"Name of the ${name} to edit."`

	SetArgs
	AddArgs
	DelArgs

	Visual bool `help:"Open an editor to modify fields visually."`
	DryRun bool `help:"Print patches without applying them."`

	FormatOpts
}

func (cmd ResourceEditCmd[R]) HelpSections() []kingkong.HelpSection {
	return ResourceCmd[R]{}.HelpSections()
}

func (cmd ResourceEditCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeEdit]
	}
	return nil
}

func (cmd *ResourceEditCmd[R]) toPatchSpec() (patch.PatchSpec, error) {
	spec := patch.PatchSpec{
		Set: make(map[string][]string),
		Add: make(map[string][]string),
		Del: make(map[string][]string),
	}
	if err := cmd.SetArgs.Apply(&spec); err != nil {
		return spec, err
	}
	if err := cmd.AddArgs.Apply(&spec); err != nil {
		return spec, err
	}
	if err := cmd.DelArgs.Apply(&spec); err != nil {
		return spec, err
	}
	return spec, nil
}

func (cmd *ResourceEditCmd[R]) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox) error {
	spec, err := cmd.toPatchSpec()
	if err != nil {
		return err
	}

	var empty R
	r := sandbox.WrapEditable(empty)
	resources, err := r.Get(ctx, []string{cmd.Name})
	if err != nil {
		return err
	}
	if len(resources) == 0 {
		return fmt.Errorf("resource not found: %s", cmd.Name)
	}
	if len(resources) > 1 {
		var keys []string
		for _, res := range resources {
			keys = append(keys, res.Key().String())
		}
		return fmt.Errorf("ambiguous resource name: %s (found %v)", cmd.Name, keys)
	}
	res := resources[0]

	allFields := make(map[string]int)
	for k := range spec.Set {
		allFields[k]++
	}
	for k := range spec.Add {
		allFields[k]++
	}
	for k := range spec.Del {
		allFields[k]++
	}
	for k, count := range allFields {
		if count > 1 {
			return fmt.Errorf("field %s has multiple patch operations", k)
		}
	}

	fields, err := res.Fields()
	if err != nil {
		return fmt.Errorf("failed to get fields: %w", err)
	}
	patched, err := patch.PatchedFields(fields, spec)
	if err != nil {
		return err
	}
	if cmd.Visual {
		patched, err = patch.VisualEdit(ctx, stdio, res, fields, patched)
		if err != nil {
			return err
		}
	}
	patched = patch.FilterPatchableFields(patched)

	if cmd.DryRun {
		return PrintPatches(stdio.Stdout, patched, false)
	}

	updated := []resource.Resource{res}
	if len(patched) > 0 {
		result, err := r.Edit(ctx, res, patched)
		if err != nil {
			return err
		}
		updated = []resource.Resource{result}
	} else {
		log.G(ctx).Warn().
			Str("resource", res.Key().String()).
			Msg("no edits made")
	}
	return Diff(ctx, stdio.Stdout, cmd.FormatOpts, empty, []resource.Resource{res}, updated)
}

type ResourceCreateCmd[R resource.CreatableResource] struct {
	SetArgs

	Visual bool `help:"Open an editor to set fields visually."`
	DryRun bool `help:"Print patches without applying them."`

	FormatOpts
}

func (cmd ResourceCreateCmd[R]) HelpSections() []kingkong.HelpSection {
	return ResourceCmd[R]{}.HelpSections()
}

func (cmd ResourceCreateCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeCreate]
	}
	return nil
}

func (cmd *ResourceCreateCmd[R]) toPatchSpec() (patch.PatchSpec, error) {
	spec := patch.PatchSpec{
		Create: true,
		Set:    make(map[string][]string),
	}
	if err := cmd.Apply(&spec); err != nil {
		return spec, err
	}
	return spec, nil
}

func (cmd *ResourceCreateCmd[R]) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox) error {
	_, err := cmd.RunResources(ctx, stdio, sandbox)
	return err
}

func (cmd *ResourceCreateCmd[R]) RunResources(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox) ([]resource.Resource, error) {
	spec, err := cmd.toPatchSpec()
	if err != nil {
		return nil, err
	}

	var empty R
	r := sandbox.WrapCreatable(empty)
	fieldsResource := resource.Resource(empty)
	if typed, ok := any(empty).(interface {
		WithType(string) resource.Resource
	}); ok {
		if values := spec.Set["type"]; len(values) > 0 {
			fieldsResource = typed.WithType(values[0])
		}
	}
	fields, err := fieldsResource.Fields()
	if err != nil {
		return nil, fmt.Errorf("failed to get fields: %w", err)
	}
	patched, err := patch.PatchedFields(fields, spec)
	if err != nil {
		return nil, err
	}

	if cmd.Visual {
		// FIXME: should allow required fields
		patched, err = patch.VisualCreate(ctx, stdio, empty, fields, patched)
		if err != nil {
			return nil, err
		}
	}

	fields = patch.FilterCreatableFields(patched)

	if cmd.DryRun {
		return nil, PrintPatches(stdio.Stdout, fields, true)
	}

	resources, opErr := r.Create(ctx, fields)
	if opErr != nil && len(resources) == 0 {
		return nil, opErr
	}
	printErr := cmd.Output.
		WithDefault(PrinterTypeKeyValue).
		Print(ctx, stdio.Stdout, cmd.Field, empty, resources...)
	if printErr != nil {
		return resources, errors.Join(opErr, printErr)
	}
	return resources, opErr
}
