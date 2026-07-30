package seclai

// Dated API versions known to this release, for use with [Options.APIVersion].
//
// The type stays a plain string so the API can add versions without an SDK
// release, but [NewClient] rejects a version this release was not built against:
// a newer one can reshape responses that this client would decode incorrectly
// rather than reject. Upgrade the module to adopt a new version, or set
// Options.AllowUnknownAPIVersion to move first and accept that risk.
//
// Treat these as what this release understands, not as everything the server
// offers — [Client.GetAPIVersion] reports the latter.
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
