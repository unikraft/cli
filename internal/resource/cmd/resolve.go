// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"

	"unikraft.com/cli/internal/resource"
	"unikraft.com/x/joinerrgroup"
)

type resolvedResource struct {
	resource.Resource
	fields []resource.Field
}

func (r resolvedResource) Fields(ctx context.Context) ([]resource.Field, error) {
	return resource.CloneFields(r.fields), nil
}

func resolveResources(ctx context.Context, resources []resource.Resource, paths []resource.FieldPath) ([]resource.Resource, error) {
	if len(resources) == 0 {
		return resources, nil
	}

	resolved := make([]resource.Resource, len(resources))
	eg := joinerrgroup.Group{}
	for i, res := range resources {
		eg.Go(func() error {
			resolvedRes, err := resolveResource(ctx, res, paths)
			if err != nil {
				return err
			}
			resolved[i] = resolvedRes
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return resolved, nil
}

func resolveResource(ctx context.Context, res resource.Resource, paths []resource.FieldPath) (resource.Resource, error) {
	fields, err := res.Fields(ctx)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return res, nil
	}

	resolved := resource.CloneFields(fields)
	if err := resource.ResolveFields(ctx, resolved, paths); err != nil {
		return nil, err
	}

	return resolvedResource{Resource: res, fields: resolved}, nil
}

func resolveAllResources(ctx context.Context, resources []resource.Resource) ([]resource.Resource, error) {
	if len(resources) == 0 {
		return resources, nil
	}

	resolved := make([]resource.Resource, len(resources))
	eg := joinerrgroup.Group{}
	for i, res := range resources {
		eg.Go(func() error {
			resolvedRes, err := resolveAllResource(ctx, res)
			if err != nil {
				return err
			}
			resolved[i] = resolvedRes
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return resolved, nil
}

func resolveAllResource(ctx context.Context, res resource.Resource) (resource.Resource, error) {
	fields, err := res.Fields(ctx)
	if err != nil {
		return nil, err
	}

	resolved := resource.CloneFields(fields)
	if err := resource.ResolveAllFields(ctx, resolved); err != nil {
		return nil, err
	}

	return resolvedResource{Resource: res, fields: resolved}, nil
}

func unwrapResource(r resource.Resource) resource.Resource {
	if rr, ok := r.(resolvedResource); ok {
		return rr.Resource
	}
	return r
}

func unwrapResources(resources []resource.Resource) []resource.Resource {
	result := make([]resource.Resource, len(resources))
	for i, r := range resources {
		result[i] = unwrapResource(r)
	}
	return result
}
