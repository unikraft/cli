// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package types

import (
	"strings"

	"github.com/distribution/reference"

	"unikraft.com/cli/internal/images"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
)

// ImageRef is a generic wrapper around a Docker image reference.
type ImageRef[T interface {
	reference.Named
	comparable
}] struct {
	Reference T
}

func (ir ImageRef[T]) MarshalText() ([]byte, error) {
	var zero T
	if ir.Reference == zero {
		return []byte{}, nil
	}
	s := images.FamiliarString(ir.Reference)
	s, _, _ = strings.Cut(s, "@")
	return []byte(s), nil
}

func (ir *ImageRef[T]) UnmarshalText(text []byte) error {
	ref, err := images.ParseNormalizedNamed(string(text))
	if err != nil {
		return err
	}
	ref = reference.TagNameOnly(ref)
	ir.Reference = ref.(T)
	return nil
}

func (ir ImageRef[T]) Link() (string, resource.Key) {
	var zero T
	if ir.Reference == zero {
		return "", nil
	}
	return "image", multimetro.Key{
		Name: ir.Reference.String(),
	}
}
