package storagebudget

import "github.com/savorum/savorum/internal/diskspace"

// PhysicalState discounts byte utilization according to retained-data headroom.
// It never changes the filesystem admission reserve.
func PhysicalState(space diskspace.Space, footprint diskspace.Footprint, warning bool) (string, bool) {
	if space.AvailableBytes <= diskspace.ReserveBytes || (space.TotalInodes != 0 && space.AvailableInodes <= diskspace.ReserveInodes) {
		return "limited", warning
	}

	if space.TotalBytes == 0 || space.AvailableBytes > space.TotalBytes || (space.TotalInodes != 0 && space.AvailableInodes > space.TotalInodes) {
		return "unknown", warning
	}

	pressure := bytePressure(space, footprint)
	byteClear := pressure <= 0.70
	byteWarning := pressure >= 0.75
	_, clearInodes := footprint.Headroom()
	inodeWarning, inodeClear := false, true

	if space.TotalInodes != 0 {
		usedInodes := space.TotalInodes - space.AvailableInodes
		inodeClear = usedInodes < space.TotalInodes/10*7+space.TotalInodes%10*7/10 || space.AvailableInodes >= clearInodes
		inodeWarning = !inodeClear && usedInodes >= space.TotalInodes-space.TotalInodes/4
	}

	if byteWarning || inodeWarning {
		warning = true
	} else if byteClear && inodeClear {
		warning = false
	}

	if warning {
		return "warning", warning
	}

	return "normal", warning
}

// Floating-point ratios are advisory; admission retains exact byte accounting.
func bytePressure(space diskspace.Space, footprint diskspace.Footprint) float64 {
	used := float64(space.TotalBytes-space.AvailableBytes) / float64(space.TotalBytes)
	weighted := 2*float64(footprint.DatabaseBytes) + float64(footprint.OriginalBytes)
	free := float64(space.AvailableBytes - diskspace.ReserveBytes)
	library := weighted / (weighted + free)

	return min(used, (used+library)/2)
}
