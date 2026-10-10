package main

import "strings"

// ignoredContentName reports whether a file or directory name is never stored,
// listed, cataloged or indexed: Clio's own temporary files (".clio-" prefix)
// and the metadata files desktop operating systems write over WebDAV and into
// ZIP archives (section 36.1).
func ignoredContentName(name string) bool {
	switch name {
	case ".DS_Store", "Thumbs.db", "desktop.ini", "__MACOSX":
		return true
	}
	return strings.HasPrefix(name, "._") || strings.HasPrefix(name, ".clio-")
}

// ignoredContentPath reports whether any segment of a slash path is ignored.
func ignoredContentPath(clean string) bool {
	for _, segment := range strings.Split(strings.Trim(clean, "/"), "/") {
		if ignoredContentName(segment) {
			return true
		}
	}
	return false
}

// rejectIgnoredContentPath refuses to create content at an ignored name.
// Reads and deletes still accept such paths so stray files can be removed.
func rejectIgnoredContentPath(clean string) *apiError {
	if ignoredContentPath(clean) {
		return invalid("This name is reserved for system files and cannot be stored")
	}
	return nil
}
