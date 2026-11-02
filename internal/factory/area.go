package factory

import "strings"

// JobArea is the part of a repository a job's changed paths touched most, so
// a surface can count jobs, rounds and questions per area. When the
// repository declares areas, it is the declared area the most paths fall
// under (an area is a path prefix ending on a segment boundary, so
// "webhooks" holds "webhooks/x.go" but not "webhooks2/x.go"), ties going to
// the first declared. When none is declared or none holds a path, it is the
// top-level directory most paths sit under, written with a trailing slash
// ("cli/"), ties going to the lexically first. Files at the repository's root
// are no area, and no paths are no area: "".
func JobArea(areas, paths []string) string {
	if area := mostDeclared(areas, paths); area != "" {
		return area
	}
	return mostTopLevel(paths)
}

func mostDeclared(areas, paths []string) string {
	best, most := "", 0
	for _, area := range areas {
		prefix := strings.TrimSuffix(area, "/") + "/"
		n := 0
		for _, p := range paths {
			if prefix != "/" && strings.HasPrefix(p, prefix) {
				n++
			}
		}
		if n > most {
			best, most = area, n
		}
	}
	return best
}

func mostTopLevel(paths []string) string {
	counts := map[string]int{}
	best := ""
	for _, p := range paths {
		dir, _, nested := strings.Cut(p, "/")
		if !nested || dir == "" {
			continue
		}
		dir += "/"
		counts[dir]++
		if n, m := counts[dir], counts[best]; best == "" || n > m || n == m && dir < best {
			best = dir
		}
	}
	return best
}
