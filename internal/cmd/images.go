// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/opencontainers/go-digest"
	"unikraft.com/cloud/sdk/controlplane"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	imagespec "unikraft.com/x/image-spec"

	"unikraft.com/cli/internal/images"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/types"
	xreference "unikraft.com/cli/internal/x/reference"
)

type ImagesCmd struct {
	cmd.ResourceCmd[ImageEntry]
	cmd.GettableResourceCmd[Image]
	cmd.DeletableResourceCmd[Image]

	List ImagesListCmd `cmd:"" help:"List images." aliases:"ls"`
	Copy ImagesCopyCmd `cmd:"" help:"Copy images."`
}

type ImagesListCmd struct {
	cmd.ResourceListCmd[ImageEntry]
}

type Image struct {
	Ref    types.ImageRef[reference.Named] `field:",short"`
	Digest digest.Digest                   `field:",long"`

	Config   ImageConfig   `field:",embed"`
	Metadata ImageMetadata `field:",long,embed"`

	Kernel      *ImageFile  `field:",long,embed"`
	KernelDebug *ImageFile  `field:"kernel.dbg,long,embed"`
	Initrd      *ImageFile  `field:",long,embed"`
	Roms        []ImageFile `field:",long,embed"`

	Image imagespec.Image `field:"-" json:"image"`
}

type ImageConfig struct {
	Platform types.Platform `field:",short"`

	Cmd []string          `field:",long"`
	Env map[string]string `field:",long"`
}

type ImageMetadata struct {
	Author  string     `field:",long"`
	Created *time.Time `field:",long"`
}

type ImageFile struct {
	Digest      digest.Digest     `field:",long"`
	MediaType   string            `field:",long"`
	Annotations map[string]string `field:",long"`
	Size        types.SizeBytes   `field:",long"`
}

func (Image) Type() resource.Type {
	return resource.Type{
		Name:  "image",
		Names: "images",
	}
}

func (i Image) Key() resource.Key {
	return staticKey(i.Ref.Reference.String())
}

func (i Image) Raw() any {
	return nil // NOTE: no platform API response associated
}

func (i Image) Fields(ctx context.Context) ([]resource.Field, error) {
	return resource.FieldsFromStruct(i)
}

func imageFileFrom(file imagespec.File) *ImageFile {
	if file == nil {
		return nil
	}
	desc, _ := file.Source()
	return &ImageFile{
		Digest:      desc.Digest,
		MediaType:   desc.MediaType,
		Annotations: desc.Annotations,
		Size:        types.SizeBytes(desc.Size),
	}
}

func imageRomsFrom(files []imagespec.File) []ImageFile {
	if len(files) == 0 {
		return nil
	}
	roms := make([]ImageFile, 0, len(files))
	for _, file := range files {
		rom := imageFileFrom(file)
		if rom == nil {
			continue
		}
		roms = append(roms, *rom)
	}
	if len(roms) == 0 {
		return nil
	}
	return roms
}

func (Image) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	access, err := images.Accessor(ctx)
	if err != nil {
		return nil, err
	}

	resources := make([]resource.Resource, 0, len(keys))
	for _, key := range keys {
		src, err := imagespec.GuessURI(key)
		if err != nil {
			return nil, fmt.Errorf("parsing image reference %q: %w", key, err)
		}
		imgs, err := access.LoadAll(ctx, src, platforms.All)
		if err != nil {
			return nil, err
		}
		defer func() {
			for _, img := range imgs {
				img.Close()
			}
		}()

		for _, img := range imgs {
			config := img.Image
			envs := make(map[string]string, len(config.Config.Env))
			for _, entry := range config.Config.Env {
				if key, val, ok := strings.Cut(entry, "="); ok {
					envs[key] = val
				}
			}

			meta := img.Metadata()
			resource := Image{
				Ref: types.ImageRef[reference.Named]{
					Reference: img.Name,
				},
				Digest: img.Descriptor.Digest,
				Config: ImageConfig{
					Cmd:      config.Config.Cmd,
					Env:      envs,
					Platform: types.Platform(config.Platform),
				},
				Metadata: ImageMetadata{
					Author:  meta.Author,
					Created: meta.Created,
				},
				Kernel:      imageFileFrom(img.Kernel),
				KernelDebug: imageFileFrom(img.KernelDebug),
				Initrd:      imageFileFrom(img.Initrd),
				Roms:        imageRomsFrom(img.Roms),
				Image:       *img,
			}
			resources = append(resources, &resource)
		}
	}
	return resources, nil
}

func (Image) Delete(ctx context.Context, targets []resource.Resource) error {
	access, err := images.Accessor(ctx)
	if err != nil {
		return err
	}

	var errs []error
	for _, target := range targets {
		var image Image
		switch typed := target.(type) {
		case Image:
			image = typed
		case *Image:
			image = *typed
		default:
			errs = append(errs, fmt.Errorf("unexpected resource type %T", target))
			continue
		}
		ref := image.Ref.Reference.String()
		uri, err := imagespec.GuessURI(ref)
		if err != nil {
			errs = append(errs, fmt.Errorf("parsing image reference %q: %w", ref, err))
			continue
		}
		if err := access.Delete(ctx, uri); err != nil {
			errs = append(errs, fmt.Errorf("deleting image %q: %w", ref, err))
		}
	}
	return errors.Join(errs...)
}

func (Image) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect an image by tag",
				Commands:    []string{"unikraft image get nginx:latest"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List all images",
				Commands:    []string{"unikraft image list"},
			},
			{
				Description: "Filter images by reference",
				Commands:    []string{`unikraft image list --filter 'ref~="/nginx"'`},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a remote image",
				Commands:    []string{"unikraft image delete unikraft.io/official/nginx:latest"},
			},
		},
	}
}

type ImageEntry struct {
	Ref    types.ImageRef[reference.NamedTagged] `field:",short"`
	Digest digest.Digest                         `field:",short"`

	Namespace string

	Canonical reference.Canonical `field:"-"`

	Image controlplane.Image `field:"-" json:"image"`
}

func (ImageEntry) Type() resource.Type {
	return resource.Type{
		Name:  "image",
		Names: "images",
	}
}

func (i ImageEntry) Key() resource.Key {
	return staticKey(i.Ref.Reference.String())
}

func (i ImageEntry) Raw() any {
	return i.Image
}

func (i ImageEntry) Fields(ctx context.Context) ([]resource.Field, error) {
	result, err := resource.FieldsFromStruct(i)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (ImageEntry) Examples() map[cmd.CmdType][]kingkong.Example {
	return Image{}.Examples()
}

func (ImageEntry) List(ctx context.Context) ([]resource.Resource, error) {
	client, err := multimetro.NewControlClient(ctx)
	if err != nil {
		return nil, err
	}

	log.G(ctx).Trace().Msg("listing images")
	resp, err := client.ListImages(ctx, controlplane.ListImagesOpts{Details: new(true)})
	if err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, nil
	}

	var results []resource.Resource
	var errs []error
	for _, image := range resp.Data.Images {
		entries, err := ImageEntry{}.load(image)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, entry := range entries {
			results = append(results, entry)
		}
	}
	return results, errors.Join(errs...)
}

func (ImageEntry) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	client, err := multimetro.NewControlClient(ctx)
	if err != nil {
		return nil, err
	}

	normalizedKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		named, err := images.ParseNormalizedNamed(key)
		if err != nil {
			return nil, fmt.Errorf("could not parse image key %q: %w", key, err)
		}
		normalizedKeys = append(normalizedKeys, named.String())
	}

	log.G(ctx).Trace().Msg("getting images")
	details := true
	resp, err := client.ListImages(ctx, controlplane.ListImagesOpts{Details: &details})
	if err != nil {
		return nil, err
	}
	if resp.Data == nil {
		refs := make(group.Refs, 0, len(normalizedKeys))
		for _, key := range normalizedKeys {
			refs = append(refs, group.Ref{Name: key})
		}
		return nil, group.ErrRefNotFound{Refs: refs}
	}

	found := make(map[string]struct{}, len(normalizedKeys))
	var results []resource.Resource
	var errs []error
	for _, image := range resp.Data.Images {
		entries, err := ImageEntry{}.load(image)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, key := range normalizedKeys {
			if _, ok := found[key]; ok {
				continue
			}
			for _, entry := range entries {
				matchRef := reference.Named(entry.Ref.Reference)
				if entry.Canonical != nil {
					matchRef = entry.Canonical
				}
				if xreference.MatchNamed(matchRef, key) {
					found[key] = struct{}{}
					results = append(results, entry)
					break
				}
			}
		}
	}

	missing := make(group.Refs, 0, len(normalizedKeys))
	for _, key := range normalizedKeys {
		if _, ok := found[key]; ok {
			continue
		}
		missing = append(missing, group.Ref{Name: key})
	}
	var missingErr error
	if len(missing) > 0 {
		missingErr = group.ErrRefNotFound{Refs: missing}
	}
	return results, errors.Join(errors.Join(errs...), missingErr)
}

func (ImageEntry) load(image controlplane.Image) ([]ImageEntry, error) {
	name := strings.TrimSpace(ptr.ZeroIfNil(image.Name))
	if name == "" {
		return nil, fmt.Errorf("image has no name")
	}
	base, err := images.ParseNormalizedNamed(name)
	if err != nil {
		return nil, fmt.Errorf("could not parse image name %q: %w", name, err)
	}
	var baseDigest digest.Digest
	if baseDigested, ok := base.(reference.Digested); ok {
		baseDigest = baseDigested.Digest()
	}
	base = reference.TrimNamed(base)

	if len(image.Tags) == 0 {
		return nil, nil
	}

	tagged := make([]reference.NamedTagged, 0, len(image.Tags))
	tagDigests := make(map[string]digest.Digest, len(image.Tags))
	for _, tag := range image.Tags {
		tagName := strings.TrimSpace(ptr.ZeroIfNil(tag.Name))
		if tagName == "" {
			continue
		}

		var taggedRef reference.NamedTagged
		if strings.Contains(tagName, "/") || strings.Contains(tagName, ":") {
			parsed, err := images.ParseNormalizedNamed(tagName)
			if err == nil {
				parsed = reference.TagNameOnly(parsed)
				if parsedTagged, ok := parsed.(reference.NamedTagged); ok {
					taggedRef = parsedTagged
				}
			}
		}
		if taggedRef == nil {
			ref, err := reference.WithTag(base, tagName)
			if err != nil {
				return nil, fmt.Errorf("could not parse image tag %q: %w", tagName, err)
			}
			taggedRef = ref
		}

		tagged = append(tagged, taggedRef)
		digestStr := strings.TrimSpace(ptr.ZeroIfNil(tag.Digest))
		if digestStr != "" {
			tagDigest, err := digest.Parse(digestStr)
			if err != nil {
				return nil, fmt.Errorf("could not parse digest %q for tag %q: %w", digestStr, tagName, err)
			}
			tagDigests[taggedRef.Tag()] = tagDigest
		}
	}

	if len(tagged) == 0 {
		return nil, fmt.Errorf("image has no tags")
	}

	// move latest to front if present
	idx := slices.IndexFunc(tagged, func(t reference.NamedTagged) bool {
		return t.Tag() == "latest"
	})
	if idx > 0 {
		latest := tagged[idx]
		tagged = append(tagged[:idx], tagged[idx+1:]...)
		tagged = append([]reference.NamedTagged{latest}, tagged...)
	}

	results := make([]ImageEntry, 0, len(tagged))
	for _, tag := range tagged {
		result := ImageEntry{
			Image: image,
		}

		tagDigest := tagDigests[tag.Tag()]
		if tagDigest == "" {
			tagDigest = baseDigest
		}
		result.Digest = tagDigest
		if tagDigest != "" {
			canonical, err := reference.WithDigest(tag, tagDigest)
			if err != nil {
				return nil, fmt.Errorf("could not create image canonical reference: %w", err)
			}
			result.Canonical = canonical
		}

		result.Ref.Reference = tag
		if ns, _, ok := strings.Cut(reference.Path(tag), "/"); ok {
			result.Namespace = ns
		}
		results = append(results, result)
	}

	return results, nil
}

type ImagesCopyCmd struct {
	Source string `arg:"" help:"Source image reference."`
	Dest   string `arg:"" help:"Destination image reference."`
}

func (cmd ImagesCopyCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Create a copy of an image",
			Commands: []string{
				"unikraft image copy unikraft.io/official/nginx:latest unikraft.io/my-user/my-nginx",
			},
		},
		{
			Description: "Upload a local image to a remote registry",
			Commands: []string{
				"unikraft image copy ./my-local-image.tar unikraft.io/my-user/my-image:1.0.0",
			},
		},
		{
			Description: "Download an image from a remote registry",
			Commands: []string{
				"unikraft image copy unikraft.io/official/redis:latest ./my-redis-image.tar",
			},
		},
	}
}

func (cmd ImagesCopyCmd) Run(ctx context.Context) error {
	access, err := images.Accessor(ctx)
	if err != nil {
		return err
	}

	src, err := imagespec.GuessURI(cmd.Source)
	if err != nil {
		return fmt.Errorf("parsing source image reference: %w", err)
	}
	dest, err := imagespec.GuessURI(cmd.Dest)
	if err != nil {
		return fmt.Errorf("parsing destination image reference: %w", err)
	}

	imgs, err := access.LoadAll(ctx, src, platforms.All)
	if err != nil {
		return fmt.Errorf("loading image from source: %w", err)
	}
	defer func() {
		for _, img := range imgs {
			img.Close()
		}
	}()

	err = access.Save(ctx, dest, imgs...)
	if err != nil {
		return fmt.Errorf("saving image to destination: %w", err)
	}

	return nil
}
