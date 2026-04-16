// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package value

import (
	"encoding"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/ettle/strcase"
)

type Wrapped interface {
	Unwrap() any
}

func Format(value any) (string, error) {
	for {
		unwrapped, ok := value.(Wrapped)
		if !ok {
			break
		}
		value = unwrapped.Unwrap()
	}

	if value == nil {
		return "", nil
	}

	// Check for nil pointers before checking for interfaces
	// because a nil pointer to a type that implements an interface
	// will pass the interface check but panic when calling methods
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return "", nil
	}

	if value, ok := value.(fmt.Stringer); ok {
		return value.String(), nil
	}
	if value, ok := value.(encoding.TextMarshaler); ok {
		dt, err := value.MarshalText()
		return string(dt), err
	}

	switch v.Kind() {
	case reflect.Pointer:
		return Format(v.Elem().Interface())
	case reflect.String:
		return v.String(), nil
	case reflect.Slice, reflect.Array:
		var result []string
		for i := range v.Len() {
			val := v.Index(i)
			valStr, err := Format(val.Interface())
			if err != nil {
				return "", err
			}
			result = append(result, valStr)
		}
		return strings.Join(result, ","), nil
	case reflect.Map:
		var result []string
		for _, key := range v.MapKeys() {
			val := v.MapIndex(key)
			keyStr, err := Format(key.Interface())
			if err != nil {
				return "", err
			}
			valStr, err := Format(val.Interface())
			if err != nil {
				return "", err
			}
			result = append(result, fmt.Sprintf("%s=%s", keyStr, valStr))
		}
		slices.Sort(result)
		return strings.Join(result, ","), nil
	case reflect.Struct:
		var result []string
		for i := range v.NumField() {
			field := v.Type().Field(i)
			if !field.IsExported() {
				continue
			}
			val := v.Field(i)
			valStr, err := Format(val.Interface())
			if err != nil {
				return "", err
			}
			if valStr == "" {
				continue
			}
			// Use "name" tag for value formatting, separate from "field" tag
			// This allows fields to be excluded from the field system (field:"-")
			// while still being formattable for --set values
			name := field.Tag.Get("name")
			if name == "-" {
				continue
			}
			if name == "" {
				name = strcase.ToKebab(field.Name)
			}
			result = append(result, fmt.Sprintf("%s=%s", name, valStr))
		}
		return strings.Join(result, ","), nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}
