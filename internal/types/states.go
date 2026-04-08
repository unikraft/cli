// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package types

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/x/colors"
)

// ANSI escape sequences for styling using charmbracelet/x/ansi.
// These use only foreground changes and reset the foreground to default (SGR 39).
var (
	fgPrimary = ansi.NewStyle().ForegroundColor(colors.Primary).String()
	fgSuccess = ansi.NewStyle().ForegroundColor(colors.Success).String()
	fgWarning = ansi.NewStyle().ForegroundColor(colors.Warning).String()
	fgError   = ansi.NewStyle().ForegroundColor(colors.Error).String()
	fgInfo    = ansi.NewStyle().ForegroundColor(colors.Info).String()
	fgReset   = ansi.NewStyle().ForegroundColor(nil).String()
)

// InstanceState is a wrapper around platform.InstanceState to automatically
// add pretty colors.
type InstanceState platform.InstanceState

func (state InstanceState) String() string {
	return state.ansiFg() + string(state) + fgReset
}

func (state InstanceState) IsRunning() bool {
	switch platform.InstanceState(state) {
	case platform.InstanceStateRunning,
		platform.InstanceStateStarting,
		platform.InstanceStateStandby:
		return true
	default:
		return false
	}
}

func (state InstanceState) validate() error {
	switch platform.InstanceState(state) {
	case platform.InstanceStateStopped:
	case platform.InstanceStateStarting:
	case platform.InstanceStateRunning:
	case platform.InstanceStateDraining:
	case platform.InstanceStateStopping:
	case platform.InstanceStateTemplate:
	case platform.InstanceStateStandby:
	default:
		return fmt.Errorf("unknown instance state: %q", string(state))
	}
	return nil
}

func (state *InstanceState) UnmarshalText(text []byte) error {
	s := InstanceState(text)
	if err := s.validate(); err != nil {
		return err
	}
	*state = s
	return nil
}

func (state InstanceState) MarshalText() ([]byte, error) {
	return []byte(state), nil
}

func (state InstanceState) ansiFg() string {
	switch platform.InstanceState(state) {
	case platform.InstanceStateStopped:
		return fgError
	case platform.InstanceStateStarting:
		return fgInfo
	case platform.InstanceStateRunning:
		return fgSuccess
	case platform.InstanceStateDraining:
		return fgWarning
	case platform.InstanceStateStopping:
		return fgWarning
	case platform.InstanceStateTemplate:
		return fgPrimary
	case platform.InstanceStateStandby:
		return fgPrimary
	}
	return fgInfo
}

type VolumeState platform.VolumeState

func (state VolumeState) String() string {
	return state.ansiFg() + string(state) + fgReset
}

func (state VolumeState) validate() error {
	switch platform.VolumeState(state) {
	case platform.VolumeStateUninitialized:
	case platform.VolumeStateInitializing:
	case platform.VolumeStateAvailable:
	case platform.VolumeStateIdle:
	case platform.VolumeStateMounted:
	case platform.VolumeStateBusy:
	case platform.VolumeStateError:
	default:
		return fmt.Errorf("unknown volume state: %q", string(state))
	}
	return nil
}

func (state *VolumeState) UnmarshalText(text []byte) error {
	s := VolumeState(text)
	if err := s.validate(); err != nil {
		return err
	}
	*state = s
	return nil
}

func (state VolumeState) MarshalText() ([]byte, error) {
	return []byte(state), nil
}

func (state VolumeState) ansiFg() string {
	// FIXME: these colors probably aren't right
	switch platform.VolumeState(state) {
	case platform.VolumeStateUninitialized:
		return fgInfo
	case platform.VolumeStateInitializing:
		return fgWarning
	case platform.VolumeStateAvailable:
		return fgSuccess
	case platform.VolumeStateIdle:
		return fgPrimary
	case platform.VolumeStateMounted:
		return fgSuccess
	case platform.VolumeStateBusy:
		return fgWarning
	case platform.VolumeStateError:
		return fgError
	}
	return fgInfo
}

type CertificateState platform.CertificateState

func (state CertificateState) String() string {
	return state.ansiFg() + string(state) + fgReset
}

func (state CertificateState) validate() error {
	switch platform.CertificateState(state) {
	case platform.CertificateStatePending:
	case platform.CertificateStateValid:
	case platform.CertificateStateError:
	default:
		return fmt.Errorf("unknown certificate state: %q", string(state))
	}
	return nil
}

func (state *CertificateState) UnmarshalText(text []byte) error {
	s := CertificateState(text)
	if err := s.validate(); err != nil {
		return err
	}
	*state = s
	return nil
}

func (state CertificateState) MarshalText() ([]byte, error) {
	return []byte(state), nil
}

func (state CertificateState) ansiFg() string {
	switch platform.CertificateState(state) {
	case platform.CertificateStatePending:
		return fgWarning
	case platform.CertificateStateValid:
		return fgSuccess
	case platform.CertificateStateError:
		return fgError
	}
	return fgInfo
}
