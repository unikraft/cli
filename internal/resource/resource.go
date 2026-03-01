// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package resource

import (
	"context"
	"fmt"
	"reflect"

	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/x/colors"
)

type Type struct {
	Name  string
	Names string
}

type Key interface {
	fmt.Stringer
}

type Resource interface {
	Type() Type
	Key() Key
	Fields() ([]Field, error)
	Raw() any
}

type GettableResource interface {
	Resource
	Get(ctx context.Context, keys []string) ([]Resource, error)
}

type ListableResource interface {
	Resource
	List(ctx context.Context) ([]Resource, error)
}

type GettableListableResource interface {
	GettableResource
	ListableResource
}

type EditableResource interface {
	GettableResource
	Edit(ctx context.Context, target Resource, fields []Field) (Resource, error)
}

type CreatableResource interface {
	GettableResource
	Create(ctx context.Context, fields []Field) ([]Resource, error)
}

type DeletableResource interface {
	GettableResource
	Delete(ctx context.Context, targets []Resource) error
}

type Link struct {
	Type string
	Key  string
}

type Field struct {
	// Name is the name of the field
	Name string `json:"name"`
	// Value contains the value of the field
	Value any `json:"value,omitempty"`
	// ValueCallback computes the field value lazily. When resolved via
	// ResolveCallbacks, the result is stored in Value and the callback is cleared.
	ValueCallback func(ctx context.Context) (any, error) `json:"-"`

	// Subfields allow defining nested structures
	Subfields []Field `json:"subfields,omitempty"`
	// Elem is used to indicate that all subfields have the same substructure
	// (e.g. for arrays)
	Elem *Field `json:"elem,omitempty"`

	Links []Link `json:"links,omitempty"`

	// display settings
	Verbosity FieldVerbosity `json:"verbosity"`
	Hyperlink string         `json:"hyperlink,omitempty"`

	// settings for creating or patching resources
	Create *Patch `json:"create,omitempty"`
	Edit   *Patch `json:"edit,omitempty"`
}

func (f Field) HasChildren() bool {
	return len(f.Subfields) > 0 || f.Elem != nil
}

func (f Field) IsEmpty() bool {
	if f.Value != nil {
		return reflect.ValueOf(f.Value).IsZero()
	}
	if f.ValueCallback != nil {
		return false // has a potential value
	}
	if f.Elem != nil {
		return len(f.Subfields) == 0
	}

	for _, subfield := range f.Subfields {
		if !subfield.IsEmpty() {
			return false
		}
	}
	return true
}

func (f Field) Get(name string) (Field, bool) {
	for _, subfield := range f.Subfields {
		if subfield.Name == name {
			return subfield, true
		}
	}
	return Field{}, false
}

type Patch struct {
	Set any `json:"set,omitempty"`
	Add any `json:"add,omitempty"`
	Del any `json:"del,omitempty"`

	// Required indicates whether a field must be provided when patching a resource.
	Required bool `json:"required,omitempty"`
}

func (f Field) Render() (string, error) {
	if f.Value == nil && f.ValueCallback != nil {
		return colors.InfoFg("<lazy>"), nil
	}
	return value.Format(f.Value)
}

type FieldVerbosity int

const (
	FieldVerbosityUnknown FieldVerbosity = iota
	FieldVerbosityInvisible
	FieldVerbosityHidden
	FieldVerbosityLong
	FieldVerbosityShort
	FieldVerbosityNone // do not show anything
)

func (v FieldVerbosity) String() string {
	if v < 0 {
		v = 0
	}
	switch v {
	case FieldVerbosityUnknown:
		return "unknown"
	case FieldVerbosityInvisible:
		return "invisible"
	case FieldVerbosityHidden:
		return "hidden"
	case FieldVerbosityLong:
		return "long"
	case FieldVerbosityShort:
		return "short"
	default:
		return "always"
	}
}

func (v FieldVerbosity) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, `%q`, v.String()), nil
}

// CompareFieldValues compares two field values in a type-aware manner.
// It returns -1 if a < b, 0 if a == b, and 1 if a > b.
func CompareFieldValues(a, b any) int {
	return value.Compare(a, b)
}
