package pagebtree

// MmapPlatformCapability describes which mmap-adjacent mechanics are compiled
// into this build. It is a build contract, not a guarantee about a particular
// filesystem, kernel version, or deployment environment.
type MmapPlatformCapability struct {
	GOOS                        string
	PageSize                    int
	MmapSupported               bool
	MmapPrimitive               string
	WriterLockSupported         bool
	ReadOnlySharedLockSupported bool
	LockPrimitive               string
	ReaderTableSupported        bool
	ProcessStartTokenSupported  bool
	BootIDTokenSupported        bool
	MadviseSupported            bool
	FileAdviceSupported         bool
	FileAdvicePrimitive         string
	CacheResidencySupported     bool
	CacheResidencyPrimitive     string
	FileSpaceStatsSupported     bool
	FileSpaceStatsPrimitive     string
	DirectorySyncSupported      bool
	HolePunch                   MmapHolePunchCapability
	UnsupportedReason           string
}

// MmapPlatformProfile reports the build-time platform envelope for mmap-backed
// trees. Use it in docs, tools, and tests to separate implemented mechanics
// from platform-specific stubs.
func MmapPlatformProfile() MmapPlatformCapability {
	profile := mmapPlatformProfile()
	profile.PageSize = PageSize
	profile.HolePunch = MmapHolePunchProfile()
	return profile
}
