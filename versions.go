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

	// APIVersion20260803 is the 2026-08-03 API version.
	APIVersion20260803 = "2026-08-03"

	// APIVersion20260821 is the 2026-08-21 API version.
	APIVersion20260821 = "2026-08-21"

	// APIVersion20260928 is the 2026-09-28 API version.
	APIVersion20260928 = "2026-09-28"

	// APIVersion20260930 is the 2026-09-30 API version.
	APIVersion20260930 = "2026-09-30"

	// APIVersion20261003 is the 2026-10-03 API version.
	APIVersion20261003 = "2026-10-03"

	// APIVersionDefault is the baseline applied to an unpinned, header-less caller.
	APIVersionDefault = APIVersion20260701

	// APIVersionLatest is the newest version known to this SDK release. It may lag the server.
	APIVersionLatest = APIVersion20261003
)

// KnownAPIVersions lists every version this release was built against, oldest first.
var KnownAPIVersions = []string{
	APIVersion20260701,
	APIVersion20260727,
	APIVersion20260803,
	APIVersion20260821,
	APIVersion20260928,
	APIVersion20260930,
	APIVersion20261003,
}

// isKnownAPIVersion reports whether v is one this release understands.
func isKnownAPIVersion(v string) bool {
	for _, k := range KnownAPIVersions {
		if k == v {
			return true
		}
	}
	return false
}
