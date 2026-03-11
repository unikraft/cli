// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package resource

import (
	"cmp"
	"context"
	"encoding"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/ettle/strcase"
	"github.com/mitchellh/mapstructure"
	"unikraft.com/cli/internal/xsync"
)

// FieldsFromStruct is a helper that converts a struct into a slice of Fields
// based on the `field` tags defined on the struct's fields.
func FieldsFromStruct(s any) (fields []Field, err error) {
	field, err := fieldFromStruct(nil, reflect.ValueOf(s))
	if err != nil {
		return nil, err
	}

	// Check if the struct implements LazyLoader
	if loader, ok := s.(LazyLoader); ok {
		wireLazyCallbacks(loader, field.Subfields)
	}

	return field.Subfields, nil
}

// LazyLoader is implemented by types whose field values should be loaded lazily.
type LazyLoader interface {
	Lazy(ctx context.Context) (any, error)
}

func fieldFromStruct(pf *ParsedField, v reflect.Value) (field *Field, err error) {
	s := v
	if s.Kind() == reflect.Pointer {
		if s.IsNil() {
			v2 := reflect.New(s.Type().Elem())
			s = v2
		}
		s = s.Elem()
	}
	if s.Kind() != reflect.Struct {
		return nil, nil
	}
	t := s.Type()

	if t.Name() != "" && pf != nil && !pf.Embed {
		return nil, nil
	}

	var fields []Field
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		fieldVal := s.Field(i)

		parsedField, err := ParseField(field)
		if err != nil {
			return nil, err
		}
		if parsedField == nil {
			continue
		}
		result := Field{
			Name:      parsedField.Name,
			Verbosity: parsedField.Verbosity,
			Value:     fieldVal.Interface(),
			Create:    parsedField.Create,
			Edit:      parsedField.Edit,
		}

		newField, err := fieldFromStruct(parsedField, fieldVal)
		if err != nil {
			return nil, err
		}
		if newField != nil {
			result.Value = newField.Value
			result.Subfields = newField.Subfields
			result.Verbosity = cmp.Or(result.Verbosity, newField.Verbosity)
		}

		newField, err = fieldFromSlice(parsedField, fieldVal)
		if err != nil {
			return nil, err
		}
		if newField != nil {
			result.Value = newField.Value
			result.Elem = newField.Elem
			result.Subfields = newField.Subfields
			result.Verbosity = cmp.Or(result.Verbosity, newField.Verbosity)
		}

		result.Verbosity = cmp.Or(result.Verbosity, FieldVerbosityHidden)
		fields = append(fields, result)
	}

	var value any
	if v, ok := v.Interface().(interface {
		encoding.TextMarshaler
		encoding.TextUnmarshaler
	}); ok {
		value = v
	}

	verbosity := FieldVerbosity(0)
	for _, f := range fields {
		verbosity = max(verbosity, f.Verbosity)
	}
	return &Field{
		Value:     value,
		Subfields: fields,
		Verbosity: verbosity,
	}, nil
}

func fieldFromSlice(pf *ParsedField, v reflect.Value) (field *Field, err error) {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v = reflect.New(v.Type().Elem())
		} else {
			v = v.Elem()
		}
	}
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return nil, nil
	}

	elemType := v.Type().Elem()
	elemVal := reflect.New(elemType).Elem()
	elem, err := fieldFromStruct(pf, elemVal)
	if err != nil {
		return nil, err
	}
	if elem == nil {
		return nil, nil
	}

	var fields []Field
	for i := range v.Len() {
		vv := v.Index(i)
		field, err := fieldFromStruct(pf, vv)
		if err != nil {
			return nil, err
		}
		if field == nil {
			continue
		}
		field.Name = strconv.Itoa(i)
		fields = append(fields, *field)
	}

	return &Field{
		Elem:      elem,
		Subfields: fields,
		Verbosity: elem.Verbosity,
	}, nil
}

type ParsedField struct {
	Name      string
	Type      reflect.Type
	Verbosity FieldVerbosity

	Embed bool

	Edit   *Patch
	Create *Patch
}

func ParseField(field reflect.StructField) (*ParsedField, error) {
	if !field.IsExported() {
		return nil, nil
	}
	tag := field.Tag.Get("field")
	if tag == "-" {
		return nil, nil
	}

	opts := strings.Split(tag, ",")
	name, opts := opts[0], opts[1:]
	if name == "" {
		name = field.Name
		name = strcase.ToKebab(name)
	}

	var verbosity FieldVerbosity
	switch {
	case slices.Contains(opts, "invisible"):
		verbosity = FieldVerbosityInvisible
	case slices.Contains(opts, "hidden"):
		verbosity = FieldVerbosityHidden
	case slices.Contains(opts, "short"):
		verbosity = FieldVerbosityShort
	case slices.Contains(opts, "long"):
		verbosity = FieldVerbosityLong
	}

	edit, err := parsePatch(field.Type, field.Tag.Get("edit"))
	if err != nil {
		return nil, err
	}
	create, err := parsePatch(field.Type, field.Tag.Get("create"))
	if err != nil {
		return nil, err
	}

	embed := slices.Contains(opts, "embed")

	return &ParsedField{
		Name:      name,
		Verbosity: verbosity,
		Type:      field.Type,
		Embed:     embed,
		Edit:      edit,
		Create:    create,
	}, nil
}

func parsePatch(tp reflect.Type, tag string) (*Patch, error) {
	if tag == "" {
		return nil, nil
	}
	parts := strings.Split(tag, ",")
	patch := &Patch{}
	for _, part := range parts {
		k, v, _ := strings.Cut(part, "=")
		var err error
		switch k {
		case "set":
			patch.Set, err = parsePatchValue(tp, v)
		case "add":
			patch.Add, err = parsePatchValue(tp, v)
		case "del":
			patch.Del, err = parsePatchValue(tp, v)
		case "required":
			patch.Required = true
		}
		if err != nil {
			return nil, err
		}
	}
	return patch, nil
}

func parsePatchValue(tp reflect.Type, value string) (any, error) {
	switch value {
	case "":
		return reflect.Zero(tp).Interface(), nil
	case "keys":
		if tp.Kind() == reflect.Map {
			return reflect.Zero(tp.Key()).Interface(), nil
		}
		return nil, fmt.Errorf("keys patch value only valid for map types")
	default:
		return nil, fmt.Errorf("unknown patch value: %s", value)
	}
}

// HACK: avoid use of this method, and prefer using the info available directly
// on the Field - this function makes heavy assumptions about the structure of
// field data and how values are read/written. Currently it is only used for
// visual editing.
func DecodeStruct(input any, output any) error {
	config := mapstructure.DecoderConfig{
		TagName:          "field",
		ErrorUnused:      true,
		Result:           output,
		WeaklyTypedInput: true,
		DecodeHook:       mapstructure.TextUnmarshallerHookFunc(),
	}
	decoder, err := mapstructure.NewDecoder(&config)
	if err != nil {
		return err
	}
	return decoder.Decode(input)
}

// wireLazyCallbacks sets up ValueCallbacks on all fields for a LazyLoader.
func wireLazyCallbacks(loader LazyLoader, fields []Field) {
	loadFields := xsync.OnceCtxValues(func(ctx context.Context) ([]Field, error) {
		populated, err := loader.Lazy(ctx)
		if err != nil {
			return nil, err
		}

		populatedFields, err := fieldFromStruct(nil, reflect.ValueOf(populated))
		if err != nil {
			return nil, err
		}
		return populatedFields.Subfields, nil
	})

	wireFieldCallbacks(loadFields, fields)
}

// wireFieldCallbacks recursively sets ValueCallbacks on fields.
func wireFieldCallbacks(loadFields func(context.Context) ([]Field, error), fields []Field) {
	for i := range fields {
		field := &fields[i]
		name := field.Name

		// Set the ValueCallback for this field
		field.ValueCallback = func(ctx context.Context) (any, error) {
			clonedFields, err := loadFields(ctx)
			if err != nil {
				return nil, err
			}
			for _, cf := range clonedFields {
				if cf.Name == name {
					return cf.Value, nil
				}
			}
			return nil, nil
		}
		field.Value = nil // Clear the value so the callback is used

		// Recursively wire up subfields with a nested loader
		if len(field.Subfields) > 0 {
			nestedLoader := func(ctx context.Context) ([]Field, error) {
				clonedFields, err := loadFields(ctx)
				if err != nil {
					return nil, err
				}
				for _, cf := range clonedFields {
					if cf.Name == name {
						return cf.Subfields, nil
					}
				}
				return nil, nil
			}
			wireFieldCallbacks(nestedLoader, field.Subfields)
		}
	}
}
