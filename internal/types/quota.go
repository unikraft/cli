// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package types

import (
	"fmt"
)

// Usage represents a used/limit pair for quota tracking.
// It marshals to text as "used/limit".
type Usage[T any] struct {
	Used  T `field:",long"`
	Limit T `field:",long"`
}

func (u Usage[T]) MarshalText() ([]byte, error) {
	return fmt.Appendf(nil, "%v/%v", u.Used, u.Limit), nil
}

func (u Usage[T]) String() string {
	return fmt.Sprintf("%v/%v", u.Used, u.Limit)
}

// Range represents a min/max range for quota limits.
// It marshals to text as "min...max".
type Range[T any] struct {
	Min T `field:",long"`
	Max T `field:",long"`
}

func (r Range[T]) MarshalText() ([]byte, error) {
	return fmt.Appendf(nil, "%v...%v", r.Min, r.Max), nil
}

func (r Range[T]) String() string {
	return fmt.Sprintf("%v...%v", r.Min, r.Max)
}
