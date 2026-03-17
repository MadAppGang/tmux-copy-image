package clipboard

import "strings"

// imageMIMEPreference is the preference order for image MIME types when
// multiple formats are available in the clipboard.
var imageMIMEPreference = []string{
	"image/png",
	"image/jpeg",
	"image/gif",
	"image/webp",
}

// selectImageType picks the best image MIME type from a list of available types.
// Returns the selected MIME type and true, or empty string and false if none found.
func selectImageType(types []string) (string, bool) {
	// Trim whitespace from each type entry.
	cleaned := make([]string, 0, len(types))
	for _, t := range types {
		if t = strings.TrimSpace(t); t != "" {
			cleaned = append(cleaned, t)
		}
	}

	// Check preferred types in order.
	for _, preferred := range imageMIMEPreference {
		for _, t := range cleaned {
			if t == preferred {
				return preferred, true
			}
		}
	}

	// Fall back to any image/* type.
	for _, t := range cleaned {
		if strings.HasPrefix(t, "image/") {
			return t, true
		}
	}

	return "", false
}
