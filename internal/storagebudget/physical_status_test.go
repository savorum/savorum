package storagebudget

import (
	"math"
	"testing"

	"github.com/savorum/savorum/internal/diskspace"
)

func TestPhysicalStateScalesWithStoredData(t *testing.T) {
	const unit = uint64(1 << 30)

	for _, tc := range []struct {
		name                               string
		total, available, database, assets uint64
		state                              string
	}{
		{"empty library on small disk", 4 * unit, unit, 0, 0, "normal"},
		{"small library on small disk", 8 * unit, 2 * unit, unit / 10, unit / 5, "normal"},
		{"small library on large disk", 1000 * unit, 250 * unit, unit, unit, "normal"},
		{"small library with eight free", 100 * unit, 8 * unit, unit, unit, "normal"},
		{"large database with eight free", 100 * unit, 8 * unit, 10 * unit, 0, "warning"},
		{"large database with twelve free", 100 * unit, 12 * unit, 10 * unit, 0, "warning"},
		{"large database with eighteen free", 100 * unit, 18 * unit, 10 * unit, 0, "normal"},
		{"large originals with eight free", 100 * unit, 8 * unit, unit, 10 * unit, "warning"},
		{"large originals with sixteen free", 100 * unit, 16 * unit, unit, 10 * unit, "normal"},
		{"large footprint preserves original trigger", 100 * unit, 25 * unit, 70 * unit, 0, "warning"},
		{"below actual utilization trigger", 100 * unit, 26 * unit, 70 * unit, 0, "normal"},
		{"headroom despite insufficient backup workspace", 100 * unit, 25 * unit, 20 * unit, 0, "normal"},
		{"hard reserve", 100 * unit, diskspace.ReserveBytes, 0, 0, "limited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, _ := PhysicalState(diskspace.Space{TotalBytes: tc.total, AvailableBytes: tc.available}, diskspace.Footprint{DatabaseBytes: tc.database, OriginalBytes: tc.assets}, false)
			if state != tc.state {
				t.Fatalf("state=%s, want %s", state, tc.state)
			}
		})
	}
}

func TestPhysicalStateWeightedPressureHysteresis(t *testing.T) {
	footprint := diskspace.Footprint{DatabaseBytes: 10 << 30}

	warning := false
	for _, tc := range []struct {
		free  uint64
		state string
	}{
		{15 << 30, "normal"},
		{8 << 30, "warning"},
		{12 << 30, "warning"},
		{15 << 30, "warning"},
		{18 << 30, "normal"},
		{18 << 30, "normal"},
		{15 << 30, "normal"},
		{8 << 30, "warning"},
		{18 << 30, "normal"},
	} {
		state, next := PhysicalState(diskspace.Space{TotalBytes: 100 << 30, AvailableBytes: tc.free}, footprint, warning)
		warning = next

		if state != tc.state {
			t.Fatalf("free=%d state=%s want=%s", tc.free, state, tc.state)
		}
	}
	// A large footprint retains the undiscounted utilization boundaries.
	footprint.DatabaseBytes = 60 << 30

	warning = false
	for _, tc := range []struct {
		free  uint64
		state string
	}{{26 << 30, "normal"}, {25 << 30, "warning"}, {29 << 30, "warning"}, {30 << 30, "normal"}, {29 << 30, "normal"}} {
		state, next := PhysicalState(diskspace.Space{TotalBytes: 100 << 30, AvailableBytes: tc.free}, footprint, warning)
		warning = next

		if state != tc.state {
			t.Fatalf("free=%d state=%s want=%s", tc.free, state, tc.state)
		}
	}
}

func TestPhysicalStateExactBytePressureBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		total, free, database uint64
		warning               bool
		state                 string
	}{
		{"above capped trigger", 100 << 30, (25 << 30) - 1, 60 << 30, false, "warning"},
		{"at capped trigger", 100 << 30, 25 << 30, 60 << 30, false, "warning"},
		{"below capped trigger", 100 << 30, (25 << 30) + 1, 60 << 30, false, "normal"},
		{"above capped recovery", 100 << 30, (30 << 30) - 1, 60 << 30, true, "warning"},
		{"at capped recovery", 100 << 30, 30 << 30, 60 << 30, true, "normal"},
		{"below capped recovery", 100 << 30, (30 << 30) + 1, 60 << 30, true, "normal"},
		{"above weighted trigger", 305 * diskspace.ReserveBytes, 61*diskspace.ReserveBytes - 1, 70 * diskspace.ReserveBytes, false, "warning"},
		{"at weighted trigger", 305 * diskspace.ReserveBytes, 61 * diskspace.ReserveBytes, 70 * diskspace.ReserveBytes, false, "warning"},
		{"below weighted trigger", 305 * diskspace.ReserveBytes, 61*diskspace.ReserveBytes + 1, 70 * diskspace.ReserveBytes, false, "normal"},
		{"above weighted recovery", 305 * diskspace.ReserveBytes, 61*diskspace.ReserveBytes - 1, 45 * diskspace.ReserveBytes, true, "warning"},
		{"at weighted recovery", 305 * diskspace.ReserveBytes, 61 * diskspace.ReserveBytes, 45 * diskspace.ReserveBytes, true, "normal"},
		{"below weighted recovery", 305 * diskspace.ReserveBytes, 61*diskspace.ReserveBytes + 1, 45 * diskspace.ReserveBytes, true, "normal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, warning := PhysicalState(diskspace.Space{TotalBytes: tc.total, AvailableBytes: tc.free}, diskspace.Footprint{DatabaseBytes: tc.database}, tc.warning)
			if state != tc.state || warning != (tc.state == "warning") {
				t.Fatalf("state=%s warning=%t, want %s", state, warning, tc.state)
			}
		})
	}
}

func TestBytePressureExamplesAndMonotonicity(t *testing.T) {
	for _, tc := range []struct {
		free, database, assets uint64
		percent                float64
	}{
		{8 << 30, 1 << 30, 1 << 30, 60.285714285714285},
		{8 << 30, 10 << 30, 0, 82.36363636363636},
		{18 << 30, 10 << 30, 0, 67.66666666666667},
	} {
		pressure := bytePressure(diskspace.Space{TotalBytes: 100 << 30, AvailableBytes: tc.free}, diskspace.Footprint{DatabaseBytes: tc.database, OriginalBytes: tc.assets})
		if math.Abs(pressure*100-tc.percent) > 1e-9 {
			t.Fatalf("free=%d database=%d assets=%d pressure=%f, want %f", tc.free, tc.database, tc.assets, pressure*100, tc.percent)
		}
	}

	for _, footprint := range []diskspace.Footprint{{}, {DatabaseBytes: 10 << 30}, {OriginalBytes: 10 << 30}, {DatabaseBytes: math.MaxUint64, OriginalBytes: math.MaxUint64}} {
		previous := 1.0

		for free := diskspace.ReserveBytes + 1; free <= 100<<30; free += 1 << 30 {
			space := diskspace.Space{TotalBytes: 100 << 30, AvailableBytes: free}
			pressure := bytePressure(space, footprint)

			utilization := float64(space.TotalBytes-free) / float64(space.TotalBytes)
			if math.IsNaN(pressure) || pressure < 0 || pressure > utilization || pressure > previous {
				t.Fatalf("footprint=%+v free=%d pressure=%f previous=%f utilization=%f", footprint, free, pressure, previous, utilization)
			}

			previous = pressure
		}
	}
}

func TestPhysicalStateInodeHeadroomScalesWithOriginals(t *testing.T) {
	footprint := diskspace.Footprint{OriginalFiles: 300}

	_, headroom := footprint.Headroom()
	for _, tc := range []struct {
		free  uint64
		state string
	}{{250, "warning"}, {300, "warning"}, {headroom, "normal"}, {32, "limited"}} {
		state, _ := PhysicalState(diskspace.Space{TotalBytes: 100 << 30, AvailableBytes: 50 << 30, TotalInodes: 2000, AvailableInodes: tc.free}, footprint, false)
		if state != tc.state {
			t.Fatalf("free=%d state=%s want=%s", tc.free, state, tc.state)
		}
	}

	state, _ := PhysicalState(diskspace.Space{TotalBytes: 100 << 30, AvailableBytes: 8 << 30, TotalInodes: 1000, AvailableInodes: 900}, diskspace.Footprint{DatabaseBytes: 10 << 30}, false)
	if state != "warning" {
		t.Fatalf("byte pressure lost: %s", state)
	}
}

func TestPhysicalStateBothPressuresMustClear(t *testing.T) {
	footprint := diskspace.Footprint{DatabaseBytes: 10 << 30, OriginalFiles: 300}
	warning := false

	for _, tc := range []struct {
		free, inodes uint64
		state        string
	}{
		{8 << 30, 1000, "warning"},
		{18 << 30, 250, "warning"},
		{18 << 30, 339, "warning"},
		{18 << 30, 340, "normal"},
		{8 << 30, 340, "warning"},
		{18 << 30, 1000, "normal"},
	} {
		state, next := PhysicalState(diskspace.Space{TotalBytes: 100 << 30, AvailableBytes: tc.free, TotalInodes: 2000, AvailableInodes: tc.inodes}, footprint, warning)
		warning = next

		if state != tc.state {
			t.Fatalf("free=%d inodes=%d state=%s, want %s", tc.free, tc.inodes, state, tc.state)
		}
	}

	state, warning := PhysicalState(diskspace.Space{TotalBytes: 100 << 30, AvailableBytes: 18 << 30}, footprint, true)
	if state != "normal" || warning {
		t.Fatal("unreported inodes blocked byte pressure recovery")
	}
}

func TestPhysicalStateUnknownAndOverflow(t *testing.T) {
	for _, space := range []diskspace.Space{{AvailableBytes: 100 << 30}, {TotalBytes: 100 << 30, AvailableBytes: 101 << 30}, {TotalBytes: 100 << 30, AvailableBytes: 50 << 30, TotalInodes: 100, AvailableInodes: 101}} {
		state, _ := PhysicalState(space, diskspace.Footprint{}, false)
		if state != "unknown" {
			t.Fatalf("state=%s", state)
		}
	}

	footprint := diskspace.Footprint{DatabaseBytes: math.MaxUint64, OriginalBytes: math.MaxUint64, OriginalFiles: math.MaxUint64}

	bytes, inodes := footprint.Headroom()
	if bytes != math.MaxUint64 || inodes != math.MaxUint64 {
		t.Fatalf("overflow reduced headroom: %d %d", bytes, inodes)
	}

	state, _ := PhysicalState(diskspace.Space{TotalBytes: 100 << 30, AvailableBytes: 8 << 30}, footprint, false)
	if state != "warning" {
		t.Fatalf("overflow cleared warning: %s", state)
	}
}
