// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package reference

import (
	"fmt"
	"strings"

	"github.com/distribution/reference"
)

const (
	dockerDomain       = "docker.io"
	legacyDockerDomain = "index.docker.io"
)

type parseOptions struct {
	defaultDomain string
	defaultPrefix string
}

type ParseOpt func(*parseOptions)

func WithDefaultDomain(domain string) ParseOpt {
	return func(opts *parseOptions) {
		opts.defaultDomain = domain
	}
}

func WithDefaultPrefix(prefix string) ParseOpt {
	return func(opts *parseOptions) {
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		opts.defaultPrefix = prefix
	}
}

// ParseNormalizedNamed is forked from github.com/distribution/reference, and
// performs the same operation, with the exception of being able to modify the default
// domain.
func ParseNormalizedNamed(s string, opts ...ParseOpt) (reference.Named, error) {
	opt := &parseOptions{
		defaultDomain: dockerDomain,
		defaultPrefix: "library/",
	}
	for _, o := range opts {
		o(opt)
	}

	domain, remainder := splitDockerDomain(s, opt)
	var remote string
	if tagSep := strings.IndexRune(remainder, ':'); tagSep > -1 {
		remote = remainder[:tagSep]
	} else {
		remote = remainder
	}
	if strings.ToLower(remote) != remote {
		return nil, fmt.Errorf("invalid reference format: repository name (%s) must be lowercase", remote)
	}

	ref, err := reference.Parse(domain + "/" + remainder)
	if err != nil {
		return nil, err
	}
	named, isNamed := ref.(reference.Named)
	if !isNamed {
		return nil, fmt.Errorf("reference %s has no name", ref.String())
	}
	return named, nil
}

const (
	localhost = `localhost`
)

// splitDockerDomain splits a repository name to domain and remote-name.
// If no valid domain is found, the default domain is used. Repository name
// needs to be already validated before.
func splitDockerDomain(name string, opt *parseOptions) (domain, remoteName string) {
	maybeDomain, maybeRemoteName, ok := strings.Cut(name, "/")
	if !ok {
		// Fast-path for single element ("familiar" names), such as "ubuntu"
		// or "ubuntu:latest". Familiar names must be handled separately, to
		// prevent them from being handled as "hostname:port".
		//
		// Canonicalize them as "docker.io/library/name[:tag]"

		// FIXME(thaJeztah): account for bare "localhost" or "example.com" names, which SHOULD be considered a domain.
		return opt.defaultDomain, opt.defaultPrefix + name
	}

	switch {
	case maybeDomain == localhost:
		// localhost is a reserved namespace and always considered a domain.
		domain, remoteName = maybeDomain, maybeRemoteName
	case maybeDomain == legacyDockerDomain:
		// canonicalize the Docker Hub and legacy "Docker Index" domains.
		domain, remoteName = dockerDomain, maybeRemoteName
	case strings.ContainsAny(maybeDomain, ".:"):
		// Likely a domain or IP-address:
		//
		// - contains a "." (e.g., "example.com" or "127.0.0.1")
		// - contains a ":" (e.g., "example:5000", "::1", or "[::1]:5000")
		domain, remoteName = maybeDomain, maybeRemoteName
	case strings.ToLower(maybeDomain) != maybeDomain:
		// Uppercase namespaces are not allowed, so if the first element
		// is not lowercase, we assume it to be a domain-name.
		domain, remoteName = maybeDomain, maybeRemoteName
	default:
		// None of the above: it's not a domain, so use the default, and
		// use the name input the remote-name.
		domain, remoteName = opt.defaultDomain, name
	}

	if (domain == dockerDomain || domain == opt.defaultDomain) && !strings.ContainsRune(remoteName, '/') {
		// Canonicalize "familiar" names, but only on Docker Hub, or the default domain
		//
		// "docker.io/ubuntu[:tag]" => "docker.io/library/ubuntu[:tag]"
		remoteName = opt.defaultPrefix + remoteName
	}

	return domain, remoteName
}

func FamiliarString(ref reference.Reference, opts ...ParseOpt) string {
	opt := &parseOptions{
		defaultDomain: dockerDomain,
		defaultPrefix: "library/",
	}
	for _, o := range opts {
		o(opt)
	}

	nn, ok := ref.(reference.Named)
	if !ok {
		return ref.String()
	}

	domain := reference.Domain(nn)
	if domain == opt.defaultDomain {
		domain = ""
	}

	path := reference.Path(nn)
	if domain == "" {
		path = strings.TrimPrefix(path, opt.defaultPrefix)
		path = strings.TrimPrefix(path, "/")
	} else {
		path = "/" + path
	}

	tag := ""
	if tagged, ok := ref.(reference.NamedTagged); ok {
		tag = tagged.Tag()
		if tag == "latest" {
			tag = ""
		} else {
			tag = ":" + tag
		}
	}

	digest := ""
	if canonical, ok := ref.(reference.Canonical); ok {
		digest = "@" + canonical.Digest().String()
	}

	return domain + path + tag + digest
}
