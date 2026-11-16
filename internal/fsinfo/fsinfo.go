// Package fsinfo contains the file-listing types shared by the local and the
// remote file systems and sent to the frontend.
package fsinfo

import (
	"io/fs"
	"sort"
	"strings"
)

// Entry is one file or directory in a listing.
type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"isDir"`
	IsLink  bool   `json:"isLink"`
	Mode    string `json:"mode"`    // e.g. "-rw-r--r--"
	Perm    uint32 `json:"perm"`    // permission bits, e.g. 0o644
	ModTime int64  `json:"modTime"` // unix seconds
}

// Crumb is one segment of a clickable path bar.
type Crumb struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Listing is the result of listing a directory.
type Listing struct {
	Path    string  `json:"path"`
	Parent  string  `json:"parent"`
	Crumbs  []Crumb `json:"crumbs"`
	Entries []Entry `json:"entries"`
}

// FromInfo builds an Entry. full is the complete path of the item.
func FromInfo(full string, fi fs.FileInfo) Entry {
	return Entry{
		Name:    fi.Name(),
		Path:    full,
		Size:    fi.Size(),
		IsDir:   fi.IsDir(),
		IsLink:  fi.Mode()&fs.ModeSymlink != 0,
		Mode:    fi.Mode().String(),
		Perm:    uint32(fi.Mode().Perm()),
		ModTime: fi.ModTime().Unix(),
	}
}

// Sort orders directories first, then by case-insensitive name.
func Sort(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

// BuildCrumbs splits a path into clickable segments using the given
// dir function (path.Dir or filepath.Dir). The root segment is named after the
// root itself ("/" or "C:\").
func BuildCrumbs(p string, dir func(string) string, base func(string) string) []Crumb {
	var rev []Crumb
	cur := p
	for {
		parent := dir(cur)
		if parent == cur {
			rev = append(rev, Crumb{Name: cur, Path: cur})
			break
		}
		rev = append(rev, Crumb{Name: base(cur), Path: cur})
		cur = parent
	}
	out := make([]Crumb, len(rev))
	for i, c := range rev {
		out[len(rev)-1-i] = c
	}
	return out
}
