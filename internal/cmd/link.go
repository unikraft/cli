// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"unikraft.com/cloud/sdk/platform/group"

	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
)

// Link models a resource reference using group.Ref-compatible fields.
type Link[T resource.Resource] struct {
	Metro string `name:"metro" json:"metro,omitempty" mirror:"metro" field:"-"`
	Name  string `name:"name" json:"name,omitempty" mirror:"name" field:",long"`
	UUID  string `name:"uuid" json:"uuid,omitempty" mirror:"uuid" field:",long"`
}

func (l Link[T]) Ref() group.Ref {
	return group.Ref{
		Metro: l.Metro,
		Name:  l.Name,
		UUID:  l.UUID,
	}
}

func (l Link[T]) Link() (string, resource.Key) {
	if l.Name == "" && l.UUID == "" {
		return "", nil
	}
	var zero T
	return zero.Type().Name, multimetro.Key{
		Metro: l.Metro,
		Name:  l.Name,
		UUID:  l.UUID,
	}
}

// MarshalText implements encoding.TextMarshaler.
// It returns the name, matching the round-trip with UnmarshalText.
func (l Link[T]) MarshalText() ([]byte, error) {
	return []byte(l.Name), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// When parsing from text, the value is stored as the name.
func (l *Link[T]) UnmarshalText(text []byte) error {
	l.Name = string(text)
	return nil
}

// LinkName models a simple name-only link.
type LinkName[T resource.Resource] string

func (l LinkName[T]) Link() (string, resource.Key) {
	if l == "" {
		return "", nil
	}
	var zero T
	return zero.Type().Name, multimetro.Key{
		Name: string(l),
	}
}

// MarshalText implements encoding.TextMarshaler.
func (l LinkName[T]) MarshalText() ([]byte, error) {
	return []byte(l), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (l *LinkName[T]) UnmarshalText(text []byte) error {
	*l = LinkName[T](text)
	return nil
}
