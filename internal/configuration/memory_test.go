package configuration

import (
	"testing"
	"testing/fstest"
)

func TestTheSmallestControlGroupMemoryLimitApplies(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		files fstest.MapFS
		want  uint64
	}{
		"cgroup v2 container": {fstest.MapFS{
			"proc/self/cgroup":         {Data: []byte("0::/\n")},
			"sys/fs/cgroup/memory.max": {Data: []byte("536870912\n")},
		}, 512 << 20},
		"cgroup v2 nested group": {fstest.MapFS{
			"proc/self/cgroup":                                      {Data: []byte("0::/system.slice/savorum.service\n")},
			"sys/fs/cgroup/system.slice/memory.max":                 {Data: []byte("1073741824\n")},
			"sys/fs/cgroup/system.slice/savorum.service/memory.max": {Data: []byte("max\n")},
		}, 1 << 30},
		"cgroup v2 without a limit": {fstest.MapFS{
			"proc/self/cgroup":         {Data: []byte("0::/\n")},
			"sys/fs/cgroup/memory.max": {Data: []byte("max\n")},
		}, 0},
		"cgroup v1 container": {fstest.MapFS{
			"proc/self/cgroup":                           {Data: []byte("12:cpu,cpuacct:/docker/abc\n4:memory:/docker/abc\n")},
			"sys/fs/cgroup/memory/memory.limit_in_bytes": {Data: []byte("268435456\n")},
		}, 256 << 20},
		"cgroup v1 without a limit": {fstest.MapFS{
			"proc/self/cgroup":                           {Data: []byte("4:memory:/\n")},
			"sys/fs/cgroup/memory/memory.limit_in_bytes": {Data: []byte("9223372036854771712\n")},
		}, 0},
		"no control groups": {fstest.MapFS{}, 0},
	}

	for name, test := range cases {
		if got := cgroupMemoryLimit(test.files); got != test.want {
			t.Errorf("%s: limit = %d, want %d", name, got, test.want)
		}
	}
}

func TestAnExplicitGoMemoryLimitIsKept(t *testing.T) {
	t.Parallel()

	host := func() uint64 { return 1000 << 20 }

	if got := memoryLimit(nil, host); got != 700<<20 {
		t.Fatalf("memory limit = %d, want seven tenths of the host", got)
	}

	if got := memoryLimit([]string{"GOMEMLIMIT=2GiB"}, host); got != 0 {
		t.Fatalf("memory limit = %d, want GOMEMLIMIT kept", got)
	}

	if got := memoryLimit(nil, func() uint64 { return 0 }); got != 0 {
		t.Fatalf("memory limit = %d with unknown host memory", got)
	}
}
