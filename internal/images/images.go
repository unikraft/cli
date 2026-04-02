// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package images

import (
	"context"
	"fmt"

	"github.com/containerd/containerd/v2/core/remotes/docker"
	"github.com/distribution/reference"
	imagespec "unikraft.com/x/image-spec"

	"unikraft.com/cli/internal/config"
	xreference "unikraft.com/cli/internal/x/reference"
)

const DefaultRegistry = "unikraft.io"

var defaultRegistries = []string{
	"unikraft.io",
	"index.unikraft.io",
}

func Accessor(ctx context.Context) (*imagespec.Accessor, error) {
	cfg := config.FromContextOrDefault(ctx)
	profile, err := cfg.CurrentProfile()
	if err != nil {
		return nil, err
	}

	options := resolverOptions(profile)
	resolver := docker.NewResolver(options)
	return imagespec.NewAccessor(
		imagespec.WithResolver(resolver),
		imagespec.WithRegistryHosts(options.Hosts),
		imagespec.WithRegistryHeaders(options.Headers),
		imagespec.WithReferenceParser(ParseNormalizedNamed),
	), nil
}

func ParseNormalizedNamed(key string) (reference.Named, error) {
	return ParseNormalizedNamedMetro(nil, key)
}

func ParseNormalizedNamedMetro(metro *config.Metro, key string) (reference.Named, error) {
	if uri, err := imagespec.ParseURI(key); err == nil {
		if uri.Scheme != imagespec.URISchemeOCI {
			return nil, fmt.Errorf("%w: invalid scheme %q", reference.ErrReferenceInvalidFormat, uri.Scheme)
		}
		key = uri.Path
	}

	index := DefaultRegistry
	if metro != nil {
		index = metro.Index().Host
	}
	return xreference.ParseNormalizedNamed(
		key,
		xreference.WithDefaultDomain(index),
		xreference.WithDefaultPrefix("official/"),
	)
}

func FamiliarString(ref reference.Reference) string {
	return xreference.FamiliarString(
		ref,
		xreference.WithDefaultDomain(DefaultRegistry),
		xreference.WithDefaultPrefix("official/"),
	)
}
