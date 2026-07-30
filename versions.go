package seclai

// Dated API versions known to this release, for use with [Options.APIVersion].
//
// The set is open: the API adds versions without an SDK release, and
// Options.APIVersion is a plain string, so a date newer than this release knows
// about can be passed directly. Treat these as convenience constants rather than
// an exhaustive list — [Client.GetAPIVersion] reports what the server supports.
const (
	// APIVersion20260701 is the 2026-07-01 API version.
	APIVersion20260701 = "2026-07-01"

	// APIVersion20260727 is the 2026-07-27 API version.
	APIVersion20260727 = "2026-07-27"

	// APIVersionDefault is the baseline applied to an unpinned, header-less caller.
	APIVersionDefault = APIVersion20260701

	// APIVersionLatest is the newest version known to this SDK release. It may lag the server.
	APIVersionLatest = APIVersion20260727
)

// KnownAPIVersions lists every version this release was built against, oldest first.
var KnownAPIVersions = []string{APIVersion20260701, APIVersion20260727}

// isKnownAPIVersion reports whether v is one this release understands.
func isKnownAPIVersion(v string) bool {
	for _, k := range KnownAPIVersions {
		if k == v {
			return true
		}
	}
	return false
}
