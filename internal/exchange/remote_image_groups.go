package exchange

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/recipe"
)

type stagedImageInfo struct {
	variantKey string
	cropBank   assets.CropHashBank
	phashValid bool
	area       uint64
	size       int
}

func collectStagedInfo(item ImportItem) []stagedImageInfo {
	infos := make([]stagedImageInfo, len(item.RemoteAssets))

	for i, remote := range item.RemoteAssets {
		identity, _ := canonicalRemoteURL(remote.URL)
		info := stagedImageInfo{
			variantKey: imageVariantKey(remote),
		}

		if staged, ok := item.stagedRemote[identity]; ok && len(staged.bytes) > 0 {
			staged.ensureMetadata()
			info.cropBank = staged.cropBank
			info.phashValid = staged.phashValid

			if staged.width > 0 && staged.height > 0 {
				info.area = uint64(staged.width) * uint64(staged.height)
			}

			info.size = len(staged.bytes)
		}

		infos[i] = info
	}

	return infos
}

func matchGroup(info stagedImageInfo, groups [][]int, infos []stagedImageInfo, item ImportItem, protected map[string]bool) int {
	for gIdx, group := range groups {
		repIdx := group[0]
		repRemote := item.RemoteAssets[repIdx]

		if !repRemote.RecipeImage || protected[repRemote.LogicalID] {
			continue
		}

		repInfo := infos[repIdx]
		if info.phashValid && repInfo.phashValid && assets.MinCropDistance(info.cropBank, repInfo.cropBank) <= 10 {
			return gIdx
		}

		if info.variantKey != "" && repInfo.variantKey != "" && info.variantKey == repInfo.variantKey {
			return gIdx
		}
	}

	return -1
}

func sortGroup(group []int, infos []stagedImageInfo) {
	slices.SortStableFunc(group, func(a, b int) int {
		infoA, infoB := infos[a], infos[b]
		if infoA.area != infoB.area {
			if infoA.area > infoB.area {
				return -1
			}

			return 1
		}

		if infoA.size != infoB.size {
			if infoA.size > infoB.size {
				return -1
			}

			return 1
		}

		return 0
	})
}

// remoteImageGroups groups remote assets representing the same photograph (e.g.
// multiple resolutions, format variants, or query-string resizes of the same image),
// ranking each group so the largest usable resolution is attempted first.
// Step images and non-recipe images remain in their own individual groups.
func remoteImageGroups(item ImportItem, document *recipe.Document) [][]int {
	var groups [][]int

	protected := stepImageIDs(document)
	infos := collectStagedInfo(item)

	for index, remote := range item.RemoteAssets {
		if !remote.RecipeImage || protected[remote.LogicalID] {
			groups = append(groups, []int{index})
			continue
		}

		if matched := matchGroup(infos[index], groups, infos, item, protected); matched >= 0 {
			groups[matched] = append(groups[matched], index)
		} else {
			groups = append(groups, []int{index})
		}
	}

	for _, group := range groups {
		sortGroup(group, infos)
	}

	return groups
}

func imageVariantKey(remote importer.RemoteAsset) string {
	if !remote.RecipeImage {
		return ""
	}

	parsed, err := url.Parse(strings.TrimSpace(remote.URL))
	if err != nil {
		return ""
	}

	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return ""
	}

	for _, key := range []string{"resize", "width", "height", "w", "h"} {
		values, exists := query[key]
		if !exists {
			continue
		}

		if len(values) != 1 || !resizeDimensions(key, values[0]) {
			return ""
		}

		query.Del(key)
	}

	parsed.RawQuery = query.Encode()

	return parsed.String() + "\x00" + remote.Caption + "\x00" + remote.Alt + "\x00" + string(remote.Role)
}

func resizeDimensions(key, value string) bool {
	parts := strings.Split(value, ",")
	if (key == "resize" && len(parts) != 2) || (key != "resize" && len(parts) != 1) {
		return false
	}

	for _, part := range parts {
		dimension, err := strconv.ParseUint(part, 10, 32)
		if err != nil || dimension == 0 {
			return false
		}
	}

	return true
}

func stepImageIDs(document *recipe.Document) map[string]bool {
	ids := map[string]bool{}

	for _, section := range document.InstructionSections {
		for _, step := range section.Steps {
			for _, id := range step.ImageRefs {
				ids[id] = true
			}
		}
	}

	return ids
}

func (s *Service) materializeImageGroup(ctx context.Context, document *recipe.Document, item ImportItem, group []int, outcomes map[string]remoteStagingOutcome) remoteMaterialization {
	result := remoteMaterializationFailed

	for _, index := range group {
		outcome := s.materializeRemoteAsset(ctx, document, item, item.RemoteAssets[index], index, outcomes)
		if outcome == remoteMaterialized {
			return outcome
		}

		if outcome == remoteMaterializationDeferred {
			result = outcome
		}
	}

	return result
}
