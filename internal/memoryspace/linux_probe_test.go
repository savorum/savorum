package memoryspace

import (
	"fmt"
	"os"
	"testing"
)

func TestLinuxPhysicalAndControllerAvailability(t *testing.T) {
	for _, test := range []struct {
		name, membership, mount, limit, current string
		want                                    Space
	}{
		{"v2", "0::/\n", "29 23 0:26 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n", "536870912", "67108864", Space{512 << 20, 448 << 20}},
		{"v1", "5:memory:/tenant\n", "29 23 0:26 /tenant /sys/fs/cgroup/memory rw - cgroup cgroup rw,memory\n", "536870912", "67108864", Space{512 << 20, 448 << 20}},
		{"unlimited", "0::/\n", "29 23 0:26 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n", "max", "", Space{2 << 30, 1 << 30}},
	} {
		t.Run(test.name, func(t *testing.T) {
			read := func(path string) ([]byte, error) {
				switch path {
				case "/proc/meminfo":
					return []byte("MemTotal: 2097152 kB\nMemAvailable: 1048576 kB\n"), nil
				case "/proc/self/cgroup":
					return []byte(test.membership), nil
				case "/proc/self/mountinfo":
					return []byte(test.mount), nil
				}

				if path == "/sys/fs/cgroup/memory.max" || path == "/sys/fs/cgroup/memory/memory.limit_in_bytes" {
					return []byte(test.limit), nil
				}

				if path == "/sys/fs/cgroup/memory.current" || path == "/sys/fs/cgroup/memory/memory.usage_in_bytes" {
					return []byte(test.current), nil
				}

				return nil, fmt.Errorf("unexpected read %s", path)
			}

			got, err := inspectLinux(read)
			if err != nil || got != test.want {
				t.Fatalf("got %+v %v; want %+v", got, err, test.want)
			}
		})
	}
}

func TestLinuxUnknownControllerFailsClosed(t *testing.T) {
	read := func(path string) ([]byte, error) {
		switch path {
		case "/proc/meminfo":
			return []byte("MemTotal: 2097152 kB\nMemAvailable: 1048576 kB\n"), nil
		case "/proc/self/cgroup":
			return []byte("0::/\n"), nil
		case "/proc/self/mountinfo":
			return []byte(""), nil
		}

		return nil, fmt.Errorf("missing %s", path)
	}
	if _, err := inspectLinux(read); err == nil {
		t.Fatal("unknown controller admitted")
	}
}

func TestLinuxNestedAncestorLimits(t *testing.T) {
	data := map[string]string{
		"/proc/meminfo":                           "MemTotal: 2097152 kB\nMemAvailable: 1048576 kB\n",
		"/proc/self/cgroup":                       "0::/tenant/worker\n",
		"/proc/self/mountinfo":                    "29 23 0:26 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
		"/sys/fs/cgroup/tenant/worker/memory.max": "max",
		"/sys/fs/cgroup/tenant/memory.max":        "1073741824",
		"/sys/fs/cgroup/tenant/memory.current":    "100663296",
		"/sys/fs/cgroup/memory.max":               "536870912",
		"/sys/fs/cgroup/memory.current":           "134217728",
	}
	read := func(path string) ([]byte, error) {
		s, ok := data[path]
		if !ok {
			return nil, fmt.Errorf("unexpected %s", path)
		}

		return []byte(s), nil
	}

	got, err := inspectLinux(read)
	if err != nil || got != (Space{512 << 20, 384 << 20}) {
		t.Fatalf("ancestor limits: %+v %v", got, err)
	}

	delete(data, "/sys/fs/cgroup/memory.current")

	if _, err := inspectLinux(read); err == nil {
		t.Fatal("unknown usage admitted")
	}

	data["/sys/fs/cgroup/memory.current"] = "not a number"

	if _, err := inspectLinux(read); err == nil {
		t.Fatal("malformed usage admitted")
	}

	data["/proc/meminfo"] = "MemTotal: 18446744073709551615 kB\nMemAvailable: 1024 kB\n"

	if _, err := inspectLinux(read); err == nil {
		t.Fatal("overflow admitted")
	}
}

func TestLinuxHierarchyRootWithoutLimitAndNamespaceRootWithLimit(t *testing.T) {
	for _, test := range []struct {
		name, membership, mount string
		files                   map[string]string
		want                    Space
	}{
		{"actual hierarchy root exempt", "0::/\n", "29 23 0:26 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n", map[string]string{}, Space{2 << 30, 1 << 30}},
		{"child cap with exempt ancestor", "0::/tenant\n", "29 23 0:26 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n", map[string]string{
			"/sys/fs/cgroup/tenant/memory.max": "536870912", "/sys/fs/cgroup/tenant/memory.current": "67108864",
		}, Space{512 << 20, 448 << 20}},
		{"namespace root cap", "0::/\n", "29 23 0:26 /tenant /sys/fs/cgroup rw - cgroup2 cgroup rw\n", map[string]string{
			"/sys/fs/cgroup/memory.max": "536870912", "/sys/fs/cgroup/memory.current": "67108864",
		}, Space{512 << 20, 448 << 20}},
	} {
		t.Run(test.name, func(t *testing.T) {
			read := func(path string) ([]byte, error) {
				switch path {
				case "/proc/meminfo":
					return []byte("MemTotal: 2097152 kB\nMemAvailable: 1048576 kB\n"), nil
				case "/proc/self/cgroup":
					return []byte(test.membership), nil
				case "/proc/self/mountinfo":
					return []byte(test.mount), nil
				}

				value, ok := test.files[path]
				if !ok {
					return nil, os.ErrNotExist
				}

				return []byte(value), nil
			}

			got, err := inspectLinux(read)
			if err != nil || got != test.want {
				t.Fatalf("got %+v %v; want %+v", got, err, test.want)
			}
		})
	}
}

func TestLinuxMissingChildOrUnreadableRootFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name, member, root string
		err                error
	}{
		{"missing child", "/tenant", "/", os.ErrNotExist},
		{"unreadable hierarchy root", "/", "/", os.ErrPermission},
		{"missing mounted namespace root", "/", "/tenant", os.ErrNotExist},
	} {
		t.Run(test.name, func(t *testing.T) {
			read := func(path string) ([]byte, error) {
				switch path {
				case "/proc/meminfo":
					return []byte("MemTotal: 2097152 kB\nMemAvailable: 1048576 kB\n"), nil
				case "/proc/self/cgroup":
					return []byte("0::" + test.member + "\n"), nil
				case "/proc/self/mountinfo":
					return []byte("29 23 0:26 " + test.root + " /sys/fs/cgroup rw - cgroup2 cgroup rw\n"), nil
				}

				return nil, test.err
			}
			if _, err := inspectLinux(read); err == nil {
				t.Fatal("unknown controller admitted")
			}
		})
	}
}
