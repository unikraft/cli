// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"unikraft.com/cloud/sdk/platform"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/types"
)

func instancesTests(t *testing.T, r *integrationRunner) {
	metroName := ""
	if r.cfg != nil {
		metroName = r.cfg.MetroName
	}

	t.Run("create", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "instance", "list"}, match: []string{`METRO\s+NAME`}},

				// Create an nginx instance
				{args: []string{
					unikraftCmd, "instance", "create",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "runtime.env=A=1,B=2,C=3",
					"--set", "autostart=true",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
				}, match: []string{`name:\s+test-`, `image:\s+nginx`, `memory:\s+128`}},

				{args: []string{unikraftCmd, "instance", "list"}, match: []string{`test-.*nginx`}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`image:\s+nginx`, `memory:\s+128`}},

				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}, match: []string{`test-`}},
				{args: []string{unikraftCmd, "instance", "list"}, match: []string{`METRO\s+NAME`}},
			})
	})

	t.Run("create-oom", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "instance", "create",
					"--output", "quiet",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "autostart=true",
					"--set", "resources.memory=16Mib",
					"--set", "resources.vcpus=1",
				}},
				{args: []string{unikraftCmd, "instance", "wait", "--until", "state==stopped", "--timeout", "10s", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			})
	})

	t.Run("connect", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				// Create an nginx instance with a service
				{args: []string{
					unikraftCmd, "instance", "create",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "runtime.env=A=1,B=2,C=3",
					"--set", "autostart=true",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
					"--set", "service.services=443:8080/tls+http",
					"--set", "service.domains=name=$UNIQ_DOMAIN",
				}},
				{
					args: []string{
						unikraftCmd, "instance", "inspect", "test-$UNIQ_INST",
						"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
					},
					captureEnv: "FQDN",
				},
				{args: []string{unikraftCmd, "instance", "wait", "--until", "state==running", "--timeout", "10s", "test-$UNIQ_INST"}},
				{args: []string{
					"curl",
					"-k",
					"--fail",
					"--silent",
					"--show-error",
					"--output", "/dev/null",
					"--write-out", `HTTP %{http_code} OK\n%header{server}\n`,
					"--retry", "10",
					"--retry-delay", "2",
					"--retry-all-errors",
					"--connect-timeout", "5",
					"--max-time", "10",
					"https://$FQDN",
				}, match: []string{`HTTP 200 OK`}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			})
	})

	t.Run("start-stop", func(t *testing.T) {
		t.Skip("start doesn't actually wait to start")

		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "instance", "create",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "runtime.env=A=1,B=2,C=3",
					"--set", "autostart=true",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
				}},

				{args: []string{unikraftCmd, "instance", "stop", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`state:\s+stop`}},

				{args: []string{unikraftCmd, "instance", "start", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`state:\s+running`}},

				{args: []string{unikraftCmd, "instance", "edit", "test-$UNIQ_INST", "--set", "state=stopped"}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`state:\s+stop`}},

				{args: []string{unikraftCmd, "instance", "edit", "test-$UNIQ_INST", "--set", "state=running"}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`state:\s+running`}},

				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			})
	})

	t.Run("edit", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "instance", "create",
					"--output", "quiet",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "runtime.args=before,first",
					"--set", "runtime.env=A=1,B=2",
					"--set", "autostart=false",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
				}},
				{args: []string{
					unikraftCmd, "instance", "edit", "test-$UNIQ_INST",
					"--output", "quiet",
					"--set", "image=redis:latest",
					"--set", "runtime.args=after,second",
					"--set", "runtime.env=A=3,B=4",
					"--set", "resources.memory=256",
				}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`image:\s+redis`, `memory:\s+256`, `A:\s+3`}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			})
	})

	t.Run("volume", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "volume", "create",
					"--output", "quiet",
					"--set", "name=test-$UNIQ_VOL",
					"--set", "size=20",
					"--set", "metro=" + metroName,
				}},
				{args: []string{
					unikraftCmd, "instance", "create",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "autostart=true",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
					"--set", "volumes=test-$UNIQ_VOL:/mnt",
				}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`:/mnt`}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "volume", "delete", "test-$UNIQ_VOL"}},
			})
	})

	t.Run("volume-inline", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "instance", "create",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "autostart=true",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
					"--set", "volumes=:/data:size=20",
				}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`:/data`}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			})
	})

	t.Run("shortcut-service-volume", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "service", "create",
					"--output", "quiet",
					"--set", "name=test-$UNIQ_SVC",
					"--set", "metro=" + metroName,
					"--set", "services=443:8080/tls+http",
				}},
				{args: []string{
					unikraftCmd, "volume", "create",
					"--output", "quiet",
					"--set", "name=test-$UNIQ_VOL",
					"--set", "size=20",
					"--set", "metro=" + metroName,
				}},
				{args: []string{
					unikraftCmd, "instance", "create",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "autostart=true",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
					"--service", "test-$UNIQ_SVC",
					"-v", "test-$UNIQ_VOL:/mnt",
				}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`:/mnt`, `service:`}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "volume", "delete", "test-$UNIQ_VOL"}},
				{args: []string{unikraftCmd, "service", "delete", "test-$UNIQ_SVC"}},
			})
	})

	t.Run("rom-attach", func(t *testing.T) {
		r.
			online().
			withContext(map[string]string{
				"romdata/hello.txt": "Hello from ROM!\n",
			}).
			run(t, []command{
				{args: []string{
					unikraftCmd, "instance", "create",
					"--output", "quiet",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "autostart=false",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
				}},
				{args: []string{
					unikraftCmd, "instance", "edit", "test-$UNIQ_INST",
					"--output", "quiet",
					"--set", "roms=dir=romdata,at=/rom",
				}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`at:\s+/rom`}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			})
	})

	t.Run("rom-add", func(t *testing.T) {
		r.
			online().
			withContext(map[string]string{
				"romdata1/hello.txt":   "Hello from ROM 1!\n",
				"romdata2/goodbye.txt": "Goodbye from ROM 2!\n",
			}).
			run(t, []command{
				{args: []string{
					unikraftCmd, "instance", "create",
					"--output", "quiet",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "autostart=false",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
					"--rom", "dir=romdata1,at=/rom1,name=rom1",
				}},
				{args: []string{
					unikraftCmd, "instance", "edit", "test-$UNIQ_INST",
					"--output", "quiet",
					"--add", "roms=dir=romdata2,at=/rom2,name=rom2",
				}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`at:\s+/rom1`, `at:\s+/rom2`}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			})
	})

	t.Run("rom-detach", func(t *testing.T) {
		r.
			online().
			withContext(map[string]string{
				"romdata/hello.txt": "Hello from ROM!\n",
			}).
			run(t, []command{
				{args: []string{
					unikraftCmd, "instance", "create",
					"--output", "quiet",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "autostart=false",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
					"--rom", "dir=romdata,at=/rom,name=myrom",
				}},
				{args: []string{
					unikraftCmd, "instance", "edit", "test-$UNIQ_INST",
					"--output", "quiet",
					"--del", "roms=name=myrom",
				}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`image:\s+nginx`}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			})
	})

	t.Run("autostart", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "instance", "create",
					"--output", "quiet",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "autostart=true",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
				}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`state:\s+(running|starting)`}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			})
	})

	t.Run("suspend", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "instance", "create",
					"--output", "quiet",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "autostart=true",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
				}},
				{args: []string{unikraftCmd, "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-$UNIQ_INST"}},

				{args: []string{unikraftCmd, "instance", "suspend", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`state:\s+(standby|stopped)`}},

				{args: []string{unikraftCmd, "instance", "start", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`state:\s+running`}},

				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			})
	})

	t.Run("add-domain", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "instance", "create",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "autostart=true",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
					"--set", "service.services=443:8080/tls+http",
				}},
				{
					args: []string{
						unikraftCmd, "instance", "inspect", "test-$UNIQ_INST",
						"--output", "template={{ .service.name }}",
					},
					captureEnv: "SERVICE_NAME",
				},
				{args: []string{
					unikraftCmd, "service", "edit", "$SERVICE_NAME",
					"--output", "quiet",
					"--add", "domains=name=$UNIQ_DOMAIN",
				}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}, match: []string{`service:`}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			})
	})
}

func instancesHelpTests(t *testing.T, unikraftPath string) {
	r := newTestEnv(t, unikraftPath)
	gild(t.Context(), t, r.cli,
		[]string{unikraftCmd, "instance", "--help"},
		[]string{unikraftCmd, "instance", "get", "--help"},
		[]string{unikraftCmd, "instance", "list", "--help"},
		[]string{unikraftCmd, "instance", "wait", "--help"},
		[]string{unikraftCmd, "instance", "create", "--help"},
		[]string{unikraftCmd, "instance", "edit", "--help"},
		[]string{unikraftCmd, "instance", "delete", "--help"},
		[]string{unikraftCmd, "instance", "template", "--help"},
		[]string{unikraftCmd, "instance", "template", "get", "--help"},
		[]string{unikraftCmd, "instance", "template", "list", "--help"},
		[]string{unikraftCmd, "instance", "template", "create", "--help"},
		[]string{unikraftCmd, "instance", "template", "edit", "--help"},
		[]string{unikraftCmd, "instance", "template", "delete", "--help"},
		[]string{unikraftCmd, "instance", "logs", "--help"},
		[]string{unikraftCmd, "instance", "start", "--help"},
		[]string{unikraftCmd, "instance", "stop", "--help"},
		[]string{unikraftCmd, "instance", "suspend", "--help"},
		[]string{unikraftCmd, "instance", "restart", "--help"},
	)
}

func instancesOutputTests(t *testing.T) {
	// Construct a fully-populated sample Instance with deterministic values.
	sample := cmd.Instance{
		MetroName: "fra",
		Name:      "my-instance",
		UUID:      "7b79b250-0658-4d10-8dc0-d854399d7e74",
		Tags:      []string{"env-prod", "team-core"},
		State:     types.InstanceState(platform.InstanceStateRunning),
	}
	require.NoError(t, sample.Image.UnmarshalText([]byte("nginx:latest")))
	sample.Runtime.Args = cmd.InstanceArgs{"arg1", "arg2"}
	sample.Runtime.Env = map[string]string{"KEY1": "val1", "KEY2": "val2"}
	sample.Resources.Memory = 256
	sample.Resources.VCPUs = 2

	service := &cmd.InstanceService{}
	service.Name = "my-service"
	service.UUID = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	service.Services = []*cmd.Service{
		{Source: 443, Destination: 8080, Handlers: []platform.ConnectionHandler{"tls", "http"}},
	}
	service.Domains = []cmd.Domain{
		{FQDN: "example.unikraft.app"},
	}
	sample.Service = service

	vol := &cmd.InstanceVolume{}
	vol.Name = "my-volume"
	vol.At = "/data"
	vol.Readonly = true
	sample.Volumes = []*cmd.InstanceVolume{vol}

	sample.Roms = []*cmd.InstanceRom{
		{Name: "my-rom", Image: "myuser/my-rom:latest", At: "/rom"},
	}

	sample.Networks = []struct {
		UUID      string `mirror:"uuid" field:",long"`
		PrivateIP string `mirror:"private_ip" field:",long"`
		MAC       string `mirror:"mac" field:",long"`
	}{
		{UUID: "net-uuid-1234", PrivateIP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:ff"},
	}

	sample.ScaleToZero = cmd.InstanceScaleToZero{
		Policy:       "on",
		Stateful:     true,
		CooldownTime: 500,
		NotifyTime:   100,
	}

	sample.Timing.Uptime = types.DurationMS(1500)
	sample.Timing.BootTime = types.DurationUS(250000)
	sample.Timing.NetTime = types.DurationUS(100000)

	sample.Restart.Policy = "always"
	sample.Restart.StartCount = 3
	sample.Restart.RestartCount = 1

	sample.Stop.Reason = "crashed"
	sample.Stop.Origin = "kernel"
	sample.Stop.ExitCode = new(uint32(1))

	gild[resource.Resource](t.Context(), t, dumpResource, sample)
}
