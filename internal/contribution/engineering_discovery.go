package contribution

import (
	"github.com/berryhill/aegis/internal/loop"
	"path"
	"sort"
)

type EngineeringDirectory struct {
	Path  string `json:"path"`
	Files int    `json:"files"`
}

// Directory discovery is derived from the complete controller inventory, not
// repository prose. Only exact package/directories are selectable, never globs.
func EngineeringDirectories(files []loop.RepositoryContextFile) []EngineeringDirectory {
	counts := map[string]int{}
	for _, f := range files {
		counts[path.Dir(f.Path)]++
	}
	result := make([]EngineeringDirectory, 0, len(counts))
	for p, n := range counts {
		result = append(result, EngineeringDirectory{p, n})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}
func NarrowEngineeringInventory(files []loop.RepositoryContextFile, directories []string) ([]loop.RepositoryContextFile, bool) {
	if len(directories) == 0 || len(directories) > 8 {
		return nil, false
	}
	seen := map[string]bool{}
	for _, d := range directories {
		if seen[d] {
			return nil, false
		}
		seen[d] = true
	}
	var result []loop.RepositoryContextFile
	found := map[string]bool{}
	for _, f := range files {
		d := path.Dir(f.Path)
		if seen[d] {
			result = append(result, f)
			found[d] = true
		}
	}
	return result, len(found) == len(seen)
}
